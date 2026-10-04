package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/config"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/s3"
	"github.com/msch128/mnema-talk/internal/sfu"
	"github.com/msch128/mnema-talk/internal/ws"
)

func main() {
	log.Println("Starting Mnema Talk server...")

	// 1. Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Configuration error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 2. Connect to PostgreSQL 17
	dbPool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("Failed to connect to PostgreSQL: %v", err)
	}
	defer dbPool.Close()

	// 3. Apply Schema Migrations
	migrationPath := "migrations/001_init.sql"
	if _, err := os.Stat(migrationPath); err == nil {
		if err := dbPool.Migrate(ctx, migrationPath); err != nil {
			log.Fatalf("Failed to run database migrations: %v", err)
		}
	}

	// 4. Ensure initial administrator exists (Herzog)
	if err := dbPool.EnsureAdminUser(ctx, cfg.AdminUsername, cfg.AdminInitialPassword); err != nil {
		log.Printf("Warning: failed to verify admin user: %v\n", err)
	}

	// 5. Initialize S3 Client (SeaweedFS / Cloud)
	s3Cli, err := s3.New(ctx, cfg)
	if err != nil {
		log.Printf("Warning: S3 client initialization warning: %v\n", err)
	}

	// 6. Start 30-Day Retention Worker
	if s3Cli != nil {
		chat.StartRetentionWorker(ctx, dbPool, s3Cli, cfg.MediaRetentionDays)
	}

	// 7. Initialize Real-Time WebSocket Hub
	hub := ws.NewHub()
	go hub.Run()

	// 8. Initialize Pion WebRTC SFU (for Voice & 4K 60fps Screensharing)
	voiceSFU, err := sfu.NewSFU(cfg.WebRTCUDPPortMin, cfg.WebRTCUDPPortMax, cfg.WebRTCNAT1to1IP)
	if err != nil {
		log.Printf("Warning: WebRTC SFU initialization warning: %v\n", err)
	}
	_ = voiceSFU

	// 9. Setup Router & Middleware
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))

	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// 10. WebSocket Endpoint
	r.Get("/api/ws", hub.HandleWebSocket(cfg.JWTSecret))

	// 11. Media Serving Endpoint (Streams S3 media directly with caching)
	if s3Cli != nil {
		r.Get("/api/media/{id}", chat.ServeMediaHandler(dbPool, s3Cli))
	}

	// 12. REST API Routes
	r.Route("/api", func(r chi.Router) {
		r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok","app":"mnema-talk","version":"1.0.0"}`))
		})

		// Authentication Routes
		r.Route("/auth", func(r chi.Router) {
			r.Post("/login", func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					Username string `json:"username"`
					Password string `json:"password"`
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					http.Error(w, `{"error":"invalid json payload"}`, http.StatusBadRequest)
					return
				}

				user, err := auth.Login(r.Context(), dbPool, req.Username, req.Password)
				if err != nil {
					http.Error(w, `{"error":"invalid username or password"}`, http.StatusUnauthorized)
					return
				}

				token, err := auth.GenerateToken(*user, cfg.JWTSecret, cfg.SessionExpiryHours)
				if err != nil {
					http.Error(w, `{"error":"failed to generate token"}`, http.StatusInternalServerError)
					return
				}

				http.SetCookie(w, &http.Cookie{
					Name:     "auth_token",
					Value:    token,
					Path:     "/",
					HttpOnly: true,
					Secure:   cfg.AppEnv == "production",
					SameSite: http.SameSiteLaxMode,
					MaxAge:   cfg.SessionExpiryHours * 3600,
				})

				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"token": token,
					"user":  user,
				})
			})

			r.Post("/register", func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					Username    string `json:"username"`
					DisplayName string `json:"display_name"`
					Password    string `json:"password"`
					InviteCode  string `json:"invite_code"`
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					http.Error(w, `{"error":"invalid json payload"}`, http.StatusBadRequest)
					return
				}

				if req.Username == "" || req.Password == "" || req.InviteCode == "" {
					http.Error(w, `{"error":"username, password and invite_code are required"}`, http.StatusBadRequest)
					return
				}

				displayName := req.DisplayName
				if displayName == "" {
					displayName = req.Username
				}

				user, err := auth.Register(r.Context(), dbPool, req.Username, displayName, req.Password, req.InviteCode)
				if err != nil {
					http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusBadRequest)
					return
				}

				token, err := auth.GenerateToken(*user, cfg.JWTSecret, cfg.SessionExpiryHours)
				if err != nil {
					http.Error(w, `{"error":"failed to generate token"}`, http.StatusInternalServerError)
					return
				}

				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"token": token,
					"user":  user,
				})
			})

			// Protected User Profile
			r.Group(func(r chi.Router) {
				r.Use(auth.Middleware(cfg.JWTSecret))
				r.Get("/me", func(w http.ResponseWriter, r *http.Request) {
					user, ok := r.Context().Value(auth.UserContextKey).(auth.User)
					if !ok {
						http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
						return
					}
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(user)
				})
			})
		})

		// Protected Chat & Channel Routes
		r.Group(func(r chi.Router) {
			r.Use(auth.Middleware(cfg.JWTSecret))

			// Channel Hierarchy (Categories & Channels)
			r.Get("/channels", func(w http.ResponseWriter, r *http.Request) {
				cats, uncat, err := chat.GetServerHierarchy(r.Context(), dbPool)
				if err != nil {
					http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"categories":    cats,
					"uncategorized": uncat,
				})
			})

			// Messages in Channel
			r.Get("/channels/{channelID}/messages", func(w http.ResponseWriter, r *http.Request) {
				chIDStr := chi.URLParam(r, "channelID")
				chID, err := uuid.Parse(chIDStr)
				if err != nil {
					http.Error(w, `{"error":"invalid channel id"}`, http.StatusBadRequest)
					return
				}

				limit := 50
				if l := r.URL.Query().Get("limit"); l != "" {
					if parsedL, err := strconv.Atoi(l); err == nil {
						limit = parsedL
					}
				}

				msgs, err := chat.GetChannelMessages(r.Context(), dbPool, chID, limit, nil)
				if err != nil {
					http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
					return
				}

				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(msgs)
			})

			r.Post("/channels/{channelID}/messages", func(w http.ResponseWriter, r *http.Request) {
				user := r.Context().Value(auth.UserContextKey).(auth.User)
				chIDStr := chi.URLParam(r, "channelID")
				chID, err := uuid.Parse(chIDStr)
				if err != nil {
					http.Error(w, `{"error":"invalid channel id"}`, http.StatusBadRequest)
					return
				}

				var req struct {
					Content string `json:"content"`
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Content == "" {
					http.Error(w, `{"error":"message content is required"}`, http.StatusBadRequest)
					return
				}

				msg, err := chat.CreateMessage(r.Context(), dbPool, chID, user.ID, req.Content)
				if err != nil {
					http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
					return
				}

				// Broadcast message to all WebSocket subscribers
				hub.BroadcastEvent("message_create", msg)

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusCreated)
				_ = json.NewEncoder(w).Encode(msg)
			})

			// Media Upload Handler
			if s3Cli != nil {
				r.Post("/channels/{channelID}/upload", chat.UploadHandler(dbPool, s3Cli, cfg.S3Bucket, 50))
			}
		})

		// Protected Admin Routes (Herzog Only)
		r.Route("/admin", func(r chi.Router) {
			r.Use(auth.Middleware(cfg.JWTSecret))
			r.Use(auth.AdminOnlyMiddleware)

			// Manage Invites
			r.Get("/invites", func(w http.ResponseWriter, r *http.Request) {
				rows, err := dbPool.Query(r.Context(), `
					SELECT id, code, max_uses, uses_count, expires_at, created_at 
					FROM invites ORDER BY created_at DESC
				`)
				if err != nil {
					http.Error(w, "database error", http.StatusInternalServerError)
					return
				}
				defer rows.Close()

				type InviteItem struct {
					ID        uuid.UUID  `json:"id"`
					Code      string     `json:"code"`
					MaxUses   *int       `json:"max_uses"`
					UsesCount int        `json:"uses_count"`
					ExpiresAt *time.Time `json:"expires_at"`
					CreatedAt time.Time  `json:"created_at"`
				}
				var items []InviteItem
				for rows.Next() {
					var inv InviteItem
					if err := rows.Scan(&inv.ID, &inv.Code, &inv.MaxUses, &inv.UsesCount, &inv.ExpiresAt, &inv.CreatedAt); err == nil {
						items = append(items, inv)
					}
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(items)
			})

			r.Post("/invites", func(w http.ResponseWriter, r *http.Request) {
				user := r.Context().Value(auth.UserContextKey).(auth.User)
				var req struct {
					Code    string `json:"code"`
					MaxUses *int   `json:"max_uses"`
				}
				_ = json.NewDecoder(r.Body).Decode(&req)
				if req.Code == "" {
					req.Code = uuid.New().String()[:8]
				}

				var invID uuid.UUID
				err := dbPool.QueryRow(r.Context(), `
					INSERT INTO invites (code, created_by, max_uses)
					VALUES ($1, $2, $3)
					RETURNING id
				`, req.Code, user.ID, req.MaxUses).Scan(&invID)
				if err != nil {
					http.Error(w, "failed to create invite code", http.StatusBadRequest)
					return
				}

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusCreated)
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"id":   invID,
					"code": req.Code,
				})
			})

			// Media Storage Dashboard & Pruning
			r.Get("/media/stats", func(w http.ResponseWriter, r *http.Request) {
				stats, err := chat.GetMediaStats(r.Context(), dbPool)
				if err != nil {
					http.Error(w, "failed to query media stats", http.StatusInternalServerError)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(stats)
			})

			r.Get("/media", func(w http.ResponseWriter, r *http.Request) {
				limit := 50
				offset := 0
				if l := r.URL.Query().Get("limit"); l != "" {
					if parsed, err := strconv.Atoi(l); err == nil {
						limit = parsed
					}
				}
				if o := r.URL.Query().Get("offset"); o != "" {
					if parsed, err := strconv.Atoi(o); err == nil {
						offset = parsed
					}
				}

				items, err := chat.ListMediaForDashboard(r.Context(), dbPool, limit, offset)
				if err != nil {
					http.Error(w, "failed to list media", http.StatusInternalServerError)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(items)
			})

			// Manual 1-Click Prune (> 30 days)
			r.Post("/media/prune", func(w http.ResponseWriter, r *http.Request) {
				if s3Cli == nil {
					http.Error(w, "S3 storage is not configured", http.StatusServiceUnavailable)
					return
				}

				days := cfg.MediaRetentionDays
				if d := r.URL.Query().Get("days"); d != "" {
					if parsed, err := strconv.Atoi(d); err == nil {
						days = parsed
					}
				}

				prunedCount, err := chat.PruneMediaOlderThan(r.Context(), dbPool, s3Cli, days)
				if err != nil {
					http.Error(w, fmt.Sprintf("pruning error: %s", err.Error()), http.StatusInternalServerError)
					return
				}

				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"status":       "success",
					"pruned_count": prunedCount,
					"cutoff_days":  days,
				})
			})

			// Delete Single Media Item
			r.Delete("/media/{id}", func(w http.ResponseWriter, r *http.Request) {
				if s3Cli == nil {
					http.Error(w, "S3 storage is not configured", http.StatusServiceUnavailable)
					return
				}
				idStr := chi.URLParam(r, "id")
				mediaID, err := uuid.Parse(idStr)
				if err != nil {
					http.Error(w, "invalid media id", http.StatusBadRequest)
					return
				}

				if err := chat.DeleteSingleMedia(r.Context(), dbPool, s3Cli, mediaID); err != nil {
					http.Error(w, fmt.Sprintf("delete error: %s", err.Error()), http.StatusInternalServerError)
					return
				}

				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
			})
		})
	})

	// 13. Start HTTP Server
	serverAddr := fmt.Sprintf("%s:%s", cfg.BindAddr, cfg.Port)
	server := &http.Server{
		Addr:         serverAddr,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		log.Printf("[HTTP] Mnema Talk listening on http://%s\n", serverAddr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("HTTP server failed: %v", err)
		}
	}()

	// 14. Graceful Shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server gracefully...")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("Server forced shutdown: %v\n", err)
	}

	log.Println("Mnema Talk stopped cleanly.")
}
