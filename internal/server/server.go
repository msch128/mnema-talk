// Package server wires configuration, storage and feature handlers into the
// HTTP router. cmd/server only loads config and calls Run.
package server

import (
	"context"
	"net/http"
	"net/url"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/bot"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/config"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/gateway"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/linkpreview"
	"github.com/msch128/mnema-talk/internal/media"
	"github.com/msch128/mnema-talk/internal/opwarn"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/roles"
	"github.com/msch128/mnema-talk/internal/sfu"
	"github.com/msch128/mnema-talk/internal/ssrf"
	"github.com/msch128/mnema-talk/internal/update"
	"github.com/msch128/mnema-talk/internal/webhook"
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
	// Retained for explicitly constructed native preview; NewRouter mounts no native routes.
	preview nativePreviewDependencies
	http.Handler
	Hub *ws.Hub
	// Webhooks is the incoming webhook handler (tests adjust its limits).
	Webhooks *webhook.Handler
	// Bots authenticates the bot API (tests adjust its limits).
	Bots *bot.Authenticator
	// Gateway is the bot gateway (tests adjust its timings).
	Gateway *gateway.Gateway
	cancel  context.CancelFunc
	// outgoing delivers outgoing webhooks; Close waits for it to settle.
	outgoing *webhook.Dispatcher
}

// Close stops background work and closes the hub's upgraded WebSockets.
func (r *Router) Close() {
	if r.Hub != nil {
		r.Hub.Close()
	}
	if r.cancel != nil {
		r.cancel()
	}
	if r.outgoing != nil {
		// Attempts in flight finish and the queue stays in the database.
		r.outgoing.Wait(webhookStopWait)
	}
}

// denyRefresh is how often the link-preview deny list (the server's own
// public address) is resolved again, and denyTimeout bounds one resolution.
const (
	denyRefresh = 10 * time.Minute
	denyTimeout = 10 * time.Second
)

// webhookStopWait bounds how long Close waits for outgoing webhook attempts in flight.
const webhookStopWait = 10 * time.Second

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
	hub.UpgradeLimit = httpx.NewRateLimiter(30, time.Minute).Named("ws_user")
	wsPerIP := httpx.NewRateLimiter(120, time.Minute).Named("ws_ip")
	hub.Version = d.Version
	permSvc := perm.NewService(d.DB)
	permSvc.SetUploadCeiling(cfg.MaxUploadMB)
	hub.SetPermissions(permSvc)
	var pub events.Publisher = hub
	if d.Events != nil {
		pub = d.Events
	}

	auditLog := &audit.Log{DB: d.DB, Actor: auditActor, Viewer: auditViewer(permSvc)}
	go auditLog.RunRetention(ctx, cfg.AuditRetentionDays, 6*time.Hour)

	authH := auth.NewHandler(sessions, pub)
	authH.Live = hub
	authH.Perms = permSvc
	authH.Audit = auditLog
	go authH.RunBanExpiry(ctx, time.Minute)
	authH.Audit = auditLog
	// `mnema-talk unlock` runs in another process and queues requests here.
	go authH.RunUnlockWorker(ctx, auth.UnlockPollInterval)
	go chat.RunSlowModePrune(ctx, d.DB, time.Hour)
	chatH := &chat.Handler{DB: d.DB, Events: pub, Online: hub, Perms: permSvc, Audit: auditLog}
	rolesH := &roles.Handler{Perms: permSvc, Events: pub}
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
		Perms:          permSvc,
	}

	// Outgoing webhooks may only reach public addresses (see internal/ssrf).
	ssrfPolicy := &ssrf.Policy{Allow: cfg.WebhookAllowPrivateTargets}
	outgoing := webhook.NewDispatcher(d.DB, ssrf.NewClient(ssrf.NewDialer(ssrfPolicy), 10*time.Second), d.Version)
	hub.Tap = outgoing.Observe
	hub.Presence = auditPresence(ctx, auditLog, d.DB)
	go outgoing.Run(ctx)
	webhookH := webhook.New(webhook.Handler{DB: d.DB, Events: pub, Media: mediaH, Perms: permSvc, Online: hub, PublicURL: cfg.PublicURL, Outgoing: outgoing, Policy: ssrfPolicy})

	botH := &bot.Handler{DB: d.DB, Perms: permSvc, Media: mediaH, Events: pub}
	botAuthn := bot.NewAuthenticator(d.DB)
	cmds := bot.NewCommands(bot.Commands{DB: d.DB, Perms: permSvc, Events: pub, Online: hub})
	gw := gateway.New(gateway.Config{Auth: botAuthn, Perms: permSvc, DB: d.DB, PublicURL: cfg.PublicURL})
	botH.OnRevoke = gw.Revoke
	gw.OnPresence = hub.SetBotPresence
	gw.OnStatus = func(id uuid.UUID, status string, a *gateway.Activity) {
		var act *ws.BotActivity
		if a != nil {
			act = &ws.BotActivity{Type: a.Type, Name: a.Name}
		}
		hub.SetBotStatus(id, status, act)
	}
	authH.OnAccountEnded = gw.RevokeUser
	cmds.Deliver = gw
	go gw.Run(ctx)
	// Both the hub and the gateway learn about permission changes, and the
	// gateway sees every event the hub publishes (as outgoing webhooks do).
	permSvc.SetNotifier(notifiers{hub, gw})
	prevTap := hub.Tap
	hub.Tap = func(ch uuid.UUID, eventType string, payload any) {
		if prevTap != nil {
			prevTap(ch, eventType, payload)
		}
		gw.Observe(ch, eventType, payload)
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
	systemH.warnings = opwarn.Default
	opsSrc := &opsSource{db: d.DB, hub: hub, sfu: d.SFU, storage: d.StorageReady, hasStore: d.Store != nil, version: systemH.version}
	systemH.ops = newOpsSampler(opsSrc)
	go systemH.ops.run(ctx)
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
			go refreshPreviewDeny(ctx, denyRefresh, deny)
		}
		previewH = &linkpreview.Handler{Fetcher: fetcher}
	}

	perUser := httpx.NewRateLimiter(600, time.Minute).Named("api_user")
	previewsPerUser := httpx.NewRateLimiter(120, time.Minute).Named("link_preview")
	uploadsPerUser := httpx.NewRateLimiter(30, time.Minute).Named("upload")

	r := chi.NewRouter()
	r.Use(
		httpx.RequestID,
		httpx.ClientIPMiddleware(trusted),
		httpx.Metrics,
		httpx.Logger,
		httpx.Recover(cfg.IsProduction()),
		httpx.SecurityHeaders(trusted, cfg.IsProduction()),
		httpx.CORS(cfg.AllowedOrigins),
	)

	r.Route("/api", func(api chi.Router) {
		// Webhook execute calls carry their own secret and no cookie.
		// The bot API reads the Authorization header only and never the cookie.
		api.Use(httpx.CSRFExcept(cfg.AllowedOrigins, func(r *http.Request) bool { return webhook.IsExecute(r) || bot.IsAPIPath(r) }))
		api.NotFound(func(w http.ResponseWriter, r *http.Request) {
			httpx.WriteError(w, httpx.ErrNotFound("route not found"))
		})
		api.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
			httpx.WriteError(w, httpx.NewAPIError(http.StatusMethodNotAllowed, httpx.CodeInvalidInput, "method not allowed"))
		})

		api.Get("/health", health(newHealthCheck(d.DB, d.StorageReady, healthCacheTTL), d.Version))
		if cfg.MetricsToken != "" {
			api.Get("/metrics", requireBearer(cfg.MetricsToken, metricsHandler(opsSrc)))
		}
		api.Get("/legal", legal(cfg))
		if cfg.APIDocs {
			// A page, not JSON: without a session it sends the browser to sign in.
			api.Get("/docs", apiDocs(sessions, web.APIDocsPage()))
		}
		api.Method(http.MethodGet, "/ws", wsPerIP.PerIP(http.HandlerFunc(hub.HandleWebSocket)))
		// Authenticated by the secret in the URL, not by a session.
		webhookH.MountPublic(api)

		// Bots authenticate with a token in the Authorization header, have
		// their own rate limits and Discord-shaped errors.
		api.Route("/bot/v1", func(b chi.Router) {
			b.NotFound(bot.NotFound)
			b.MethodNotAllowed(bot.MethodNotAllowed)
			// The gateway authenticates itself (header or IDENTIFY).
			b.Get("/gateway", gw.Handle)
			b.Group(func(a chi.Router) {
				a.Use(botAuthn.Middleware)
				send := botAuthn.LimitSends(bot.ChannelParam)
				a.Get("/users/@me", bot.Me)
				a.Group(func(j chi.Router) {
					j.Use(httpx.MaxBodyDiscord(httpx.DefaultMaxBody))
					chatH.MountBot(j, send)
					cmds.MountBot(j)
				})
				mediaH.MountBot(a, send)
			})
		})

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
				cmds.MountMember(j)
				mediaH.Mount(j)
				rolesH.Mount(j)
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
				adm.Use(auth.RequireAnyPerm(permSvc, perm.AdminArea), httpx.MaxBody(httpx.DefaultMaxBody))
				adm.Use(auditLog.Middleware)
				auditLog.Mount(adm.With(auth.RequirePerm(permSvc, perm.ViewAuditLog)))
				authH.MountAdmin(adm)
				chatH.MountAdmin(adm)
				rolesH.MountAdmin(adm)
				webhookH.MountAdmin(adm)
				botH.MountAdmin(adm)
				mediaH.MountAdmin(adm)
				systemH.mountAdmin(adm)
			})
		})
	})

	r.Handle("/*", web.Handler())
	return &Router{Handler: r, Hub: hub, Webhooks: webhookH, Bots: botAuthn, Gateway: gw, cancel: cancel, outgoing: outgoing, preview: nativePreviewDependencies{accounts: authH, chat: chatH, system: systemH, perUser: perUser, trusted: trusted}}, nil
}
