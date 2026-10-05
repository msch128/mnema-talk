// Package server wires configuration, storage and feature handlers into the
// HTTP router. cmd/server only loads config and calls Run.
package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/config"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/linkpreview"
	"github.com/msch128/mnema-talk/internal/media"
	"github.com/msch128/mnema-talk/internal/sfu"
	"github.com/msch128/mnema-talk/internal/ws"
	"github.com/msch128/mnema-talk/web"
)

// Deps are the collaborators the router needs. Tests build them with a test
// database, an in-memory store and no SFU.
type Deps struct {
	Config *config.Config
	DB     *db.Pool
	Store  media.Store // nil disables uploads
	SFU    *sfu.SFU    // nil disables voice
	// Events overrides the publisher (tests); nil uses the WebSocket hub.
	Events events.Publisher
	// Version is the build version reported by /api/health.
	Version string
}

// Router holds the assembled handler and the hub (for shutdown/tests).
type Router struct {
	http.Handler
	Hub *ws.Hub
}

func NewRouter(d Deps) (*Router, error) {
	cfg := d.Config
	trusted, err := httpx.ParseCIDRs(cfg.TrustedProxies)
	if err != nil {
		return nil, err
	}

	sessions := &auth.Sessions{
		DB:     d.DB,
		Secret: cfg.JWTSecret,
		TTL:    time.Duration(cfg.SessionExpiryHours) * time.Hour,
		Secure: cfg.SecureCookies(),
	}
	hub := ws.NewHub(d.DB, sessions, d.SFU, cfg.AllowedOrigins)
	var pub events.Publisher = hub
	if d.Events != nil {
		pub = d.Events
	}

	authH := auth.NewHandler(sessions, pub)
	authH.Live = hub
	chatH := &chat.Handler{DB: d.DB, Events: pub, Online: hub}
	if d.Store != nil {
		chatH.Objects = d.Store
	}
	mediaH := &media.Handler{
		DB:             d.DB,
		Store:          d.Store,
		Bucket:         cfg.S3Bucket,
		Events:         pub,
		MaxUploadBytes: int64(cfg.MaxUploadMB) << 20,
		RetentionDays:  cfg.MediaRetentionDays,
		Online:         hub,
	}

	var previewH *linkpreview.Handler
	if cfg.LinkPreviews {
		fetcher := linkpreview.New()
		// Our own public address leads back into the LAN through the
		// router (hairpin NAT); never let a preview fetch go there.
		if u, err := url.Parse(cfg.PublicURL); err == nil {
			deny := func() { fetcher.Deny(context.Background(), u.Hostname(), cfg.WebRTCNAT1to1IP) }
			deny()
			// A home connection's public IP changes; keep the list current.
			go func() {
				for range time.Tick(10 * time.Minute) {
					deny()
				}
			}()
		}
		previewH = &linkpreview.Handler{Fetcher: fetcher}
	}

	perUser := httpx.NewRateLimiter(600, time.Minute)
	previewsPerUser := httpx.NewRateLimiter(120, time.Minute)
	uploadsPerUser := httpx.NewRateLimiter(30, time.Minute)

	r := chi.NewRouter()
	r.Use(
		httpx.RequestID,
		httpx.ClientIPMiddleware(trusted),
		httpx.Logger,
		httpx.Recover(cfg.IsProduction()),
		httpx.SecurityHeaders(trusted, cfg.IsProduction()),
		httpx.CORS(cfg.AllowedOrigins),
	)

	r.Route("/api", func(api chi.Router) {
		api.Use(httpx.CSRF(cfg.AllowedOrigins))
		api.NotFound(func(w http.ResponseWriter, r *http.Request) {
			httpx.WriteError(w, httpx.ErrNotFound("route not found"))
		})
		api.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
			httpx.WriteError(w, httpx.NewAPIError(http.StatusMethodNotAllowed, httpx.CodeInvalidInput, "method not allowed"))
		})

		api.Get("/health", health(d.DB, d.Version))
		if cfg.MetricsToken != "" {
			api.Get("/metrics", requireBearer(cfg.MetricsToken, metricsHandler(d.DB, hub, d.SFU)))
		}
		api.Get("/legal", legal(cfg))
		api.Get("/ws", hub.HandleWebSocket)

		api.Group(func(pub chi.Router) {
			pub.Use(httpx.MaxBody(httpx.DefaultMaxBody))
			authH.MountPublic(pub)
		})

		api.Group(func(authed chi.Router) {
			authed.Use(sessions.RequireUser, perUser.By(auth.UserKey))

			authed.Group(func(j chi.Router) {
				j.Use(httpx.MaxBody(httpx.DefaultMaxBody))
				authH.MountAuthenticated(j)
				j.Get("/webrtc/config", webrtcConfig(cfg))
				chatH.Mount(j)
				mediaH.Mount(j)
			})

			// Link cards make outbound requests; they get their own budget.
			if previewH != nil {
				authed.Group(func(lp chi.Router) {
					lp.Use(previewsPerUser.By(auth.UserKey))
					previewH.Mount(lp)
				})
			}

			// Uploads need a larger body ceiling than the JSON default.
			authed.Group(func(u chi.Router) {
				u.Use(httpx.MaxBody(mediaH.UploadBodyLimit()), uploadsPerUser.By(auth.UserKey))
				mediaH.MountUploads(u)
			})

			authed.Route("/admin", func(adm chi.Router) {
				adm.Use(auth.RequireAdmin, httpx.MaxBody(httpx.DefaultMaxBody))
				authH.MountAdmin(adm)
				chatH.MountAdmin(adm)
				mediaH.MountAdmin(adm)
			})
		})
	})

	r.Handle("/*", web.Handler())
	return &Router{Handler: r, Hub: hub}, nil
}

func health(p *db.Pool, version string) http.HandlerFunc {
	if version == "" {
		version = "dev"
	}
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := p.Ping(ctx); err != nil {
			httpx.WriteError(w, httpx.ErrUnavailable("database unavailable"))
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": version})
	}
}

// Run starts the HTTP server and blocks until ctx is cancelled, then shuts
// down gracefully.
func Run(ctx context.Context, cfg *config.Config, handler http.Handler) error {
	srv := &http.Server{
		Addr:              net.JoinHostPort(cfg.BindAddr, cfg.Port),
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		// Generous body/response timeouts: uploads and video downloads can be
		// large; WebSockets set their own deadlines after the upgrade.
		ReadTimeout:    10 * time.Minute,
		WriteTimeout:   10 * time.Minute,
		IdleTimeout:    120 * time.Second,
		MaxHeaderBytes: 64 << 10,
		ErrorLog:       slog.NewLogLogger(slog.Default().Handler(), slog.LevelWarn),
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", srv.Addr, "public_url", cfg.PublicURL)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("http server: %w", err)
		}
		close(errCh)
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}
	slog.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
