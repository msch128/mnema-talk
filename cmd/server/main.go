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
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/config"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/s3"
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

	// 4. Ensure initial administrator exists
	if err := dbPool.EnsureAdminUser(ctx, cfg.AdminUsername, cfg.AdminInitialPassword); err != nil {
		log.Printf("Warning: failed to verify admin user: %v\n", err)
	}

	// 5. Initialize S3 Client (SeaweedFS / Cloud)
	s3Cli, err := s3.New(ctx, cfg)
	if err != nil {
		log.Printf("Warning: S3 client initialization warning: %v\n", err)
	}
	_ = s3Cli // Available for handlers

	// 6. Setup Router & Middleware
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

	// 7. Base API Routes
	r.Route("/api", func(r chi.Router) {
		r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok","app":"mnema-talk","version":"1.0.0"}`))
		})

		// Authentication Routes
		r.Route("/auth", func(r chi.Router) {
			// POST /api/auth/login
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

			// POST /api/auth/register (Requires invite code)
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

			// Protected /api/auth/me
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
	})

	// 8. Start HTTP Server
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

	// 9. Graceful Shutdown
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
