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
	"sync"
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
	"github.com/msch128/mnema-talk/internal/update"
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
	// Updates is the release checker (nil when UPDATE_CHECK_ENABLED=false;
	// then nothing is ever requested from GitHub). The caller runs its loop.
	Updates *update.Checker
	// StorageReady reports whether object storage is usable (nil = not
	// checked); /api/health answers 503 while it returns an error.
	StorageReady func() error
	// Context bounds the router's background work (deny-list refresh);
	// nil = until Close.
	Context context.Context
}

// Router holds the assembled handler and the hub (for shutdown/tests).
type Router struct {
	http.Handler
	Hub    *ws.Hub
	cancel context.CancelFunc
}

// Close stops background work and closes the hub's upgraded WebSockets.
func (r *Router) Close() {
	if r.Hub != nil {
		r.Hub.Close()
	}
	if r.cancel != nil {
		r.cancel()
	}
}

// denyRefresh is how often the link-preview deny list (the server's own
// public address) is resolved again, and denyTimeout bounds one resolution.
const (
	denyRefresh = 10 * time.Minute
	denyTimeout = 10 * time.Second
)

func NewRouter(d Deps) (*Router, error) {
	cfg := d.Config
	trusted, err := httpx.ParseCIDRs(cfg.TrustedProxies)
	if err != nil {
		return nil, err
	}
	parent := d.Context
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)

	sessions := &auth.Sessions{
		DB:     d.DB,
		Secret: cfg.JWTSecret,
		TTL:    time.Duration(cfg.SessionExpiryHours) * time.Hour,
		Secure: cfg.SecureCookies(),
	}
	hub := ws.NewHub(d.DB, sessions, d.SFU, cfg.AllowedOrigins)
	hub.Version = d.Version
	hub.MaxRoomPeers = cfg.WebRTCMaxRoomPeers
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

	systemH := &systemHandler{
		cfg:      cfg,
		db:       d.DB,
		hub:      hub,
		sfu:      d.SFU,
		storage:  d.StorageReady,
		hasStore: d.Store != nil,
		version:  d.Version,
		started:  time.Now(),
		updates:  d.Updates,
	}
	if systemH.version == "" {
		systemH.version = "dev"
	}
	systemH.events = pub
	systemH.self = selfUpdate{confirm: authH.ConfirmPassword, now: time.Now}
	if cfg.SelfUpdateConfigured() {
		u, err := update.NewUpdater(cfg.UpdaterURL, cfg.UpdaterToken)
		if err != nil {
			cancel()
			return nil, err
		}
		systemH.self.updater = u
	}

	var previewH *linkpreview.Handler
	if cfg.LinkPreviews {
		fetcher := linkpreview.New()
		// Our own public address leads back into the LAN through the
		// router (hairpin NAT); never let a preview fetch go there.
		if u, err := url.Parse(cfg.PublicURL); err == nil {
			hosts := append([]string{u.Hostname()}, cfg.WebRTCAnnounce...)
			deny := func() {
				dctx, dcancel := context.WithTimeout(ctx, denyTimeout)
				defer dcancel()
				fetcher.Deny(dctx, hosts...)
			}
			deny()
			// A home connection's public IP changes; keep the list current.
			go func() {
				t := time.NewTicker(denyRefresh)
				defer t.Stop()
				for {
					select {
					case <-ctx.Done():
						return
					case <-t.C:
						deny()
					}
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

		api.Get("/health", health(newHealthCheck(d.DB, d.StorageReady, healthCacheTTL), d.Version))
		if cfg.MetricsToken != "" {
			api.Get("/metrics", requireBearer(cfg.MetricsToken, metricsHandler(d.DB, hub, d.SFU)))
		}
		api.Get("/legal", legal(cfg))
		if cfg.APIDocs {
			// A page, not JSON: without a session it sends the browser to sign in.
			api.Get("/docs", apiDocs(sessions))
		}
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
				if cfg.APIDocs {
					j.Get("/openapi.json", openAPISpec)
				}
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
				systemH.mountAdmin(adm)
			})
		})
	})

	r.Handle("/*", web.Handler())
	return &Router{Handler: r, Hub: hub, cancel: cancel}, nil
}

// Health is the response of GET /api/health.
type Health struct {
	Status  string `json:"status" enums:"ok"`
	Version string `json:"version"`
}

// healthCacheTTL is how long a health result is reused: the endpoint is
// public and unauthenticated, so callers must not be able to turn it into a
// stream of database pings.
const healthCacheTTL = 2 * time.Second

// healthCheck pings the database (and checks object storage) at most once
// per ttl; concurrent callers wait for the one check in flight.
type healthCheck struct {
	ping    func(context.Context) error
	storage func() error
	ttl     time.Duration

	mu      sync.Mutex
	checked time.Time
	err     *httpx.APIError
}

func newHealthCheck(p *db.Pool, storage func() error, ttl time.Duration) *healthCheck {
	ping := func(context.Context) error { return errors.New("no database") }
	if p != nil {
		ping = p.Ping
	}
	return &healthCheck{ping: ping, storage: storage, ttl: ttl}
}

// check returns nil when healthy, otherwise the error to answer with.
func (h *healthCheck) check(ctx context.Context) *httpx.APIError {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.checked.IsZero() && time.Since(h.checked) < h.ttl {
		return h.err
	}
	// Not tied to one caller: its result is shared with the others.
	pctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	h.err = nil
	if err := h.ping(pctx); err != nil {
		h.err = httpx.ErrUnavailable("database unavailable")
	} else if h.storage != nil {
		if err := h.storage(); err != nil {
			h.err = httpx.ErrUnavailable("file storage unavailable")
		}
	}
	h.checked = time.Now()
	return h.err
}

// health handles GET /api/health. The result is cached for healthCacheTTL,
// and an object storage outage answers 503 as well.
//
// @Summary Health check
// @Description Pings the database.
// @ID getHealth
// @Tags System
// @Produce json
// @Success 200 {object} Health "Healthy."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Failure 503 {object} httpx.ErrorResponse "UNAVAILABLE: a dependency (database, file storage) is not available."
// @Router /api/health [get]
func health(hc *healthCheck, version string) http.HandlerFunc {
	if version == "" {
		version = "dev"
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if err := hc.check(r.Context()); err != nil {
			httpx.WriteError(w, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, Health{Status: "ok", Version: version})
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
