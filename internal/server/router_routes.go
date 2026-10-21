package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/bot"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/webhook"
	"github.com/msch128/mnema-talk/web"
)

// buildHandler assembles the global middleware, the /api tree and the web app.
func (c *components) buildHandler() http.Handler {
	cfg := c.cfg
	r := chi.NewRouter()
	r.Use(
		httpx.RequestID,
		httpx.ClientIPMiddleware(c.trusted),
		httpx.Metrics,
		httpx.Logger,
		httpx.Recover(cfg.IsProduction()),
		httpx.SecurityHeaders(c.trusted, cfg.IsProduction()),
		httpx.CORS(cfg.AllowedOrigins),
	)
	r.Route("/api", c.mountAPI)
	r.Handle("/*", web.Handler())
	return r
}

func (c *components) mountAPI(api chi.Router) {
	cfg := c.cfg
	// Webhook execute calls carry their own secret and no cookie.
	// The bot API reads the Authorization header only and never the cookie.
	api.Use(httpx.CSRFExcept(cfg.AllowedOrigins, func(r *http.Request) bool { return webhook.IsExecute(r) || bot.IsAPIPath(r) }))
	api.NotFound(func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteError(w, httpx.ErrNotFound("route not found"))
	})
	api.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteError(w, httpx.NewAPIError(http.StatusMethodNotAllowed, httpx.CodeInvalidInput, "method not allowed"))
	})

	api.Get("/health", health(newHealthCheck(c.d.DB, c.d.StorageReady, healthCacheTTL), c.d.Version))
	if cfg.MetricsToken != "" {
		api.Get("/metrics", requireBearer(cfg.MetricsToken, metricsHandler(c.opsSrc)))
	}
	api.Get("/legal", legal(cfg))
	if cfg.APIDocs {
		// A page, not JSON: without a session it sends the browser to sign in.
		api.Get("/docs", apiDocs(c.sessions, web.APIDocsPage()))
	}
	api.Method(http.MethodGet, "/ws", c.wsPerIP.PerIP(http.HandlerFunc(c.hub.HandleWebSocket)))
	// Authenticated by the secret in the URL, not by a session.
	c.webhookH.MountPublic(api)

	// Bots authenticate with a token in the Authorization header, have
	// their own rate limits and Discord-shaped errors.
	api.Route("/bot/v1", c.mountBotAPI)

	api.Group(func(pub chi.Router) {
		pub.Use(httpx.MaxBody(httpx.DefaultMaxBody))
		c.authH.MountPublic(pub)
		c.onboardH.MountPublic(pub)
	})

	api.Group(c.mountAuthenticated)
}

func (c *components) mountBotAPI(b chi.Router) {
	b.NotFound(bot.NotFound)
	b.MethodNotAllowed(bot.MethodNotAllowed)
	// The gateway authenticates itself (header or IDENTIFY).
	b.Get("/gateway", c.gw.Handle)
	b.Group(func(a chi.Router) {
		a.Use(c.botAuthn.Middleware)
		send := c.botAuthn.LimitSends(bot.ChannelParam)
		a.Get("/users/@me", bot.Me)
		a.Group(func(j chi.Router) {
			j.Use(httpx.MaxBodyDiscord(httpx.DefaultMaxBody))
			c.chatH.MountBot(j, send)
			c.cmds.MountBot(j)
		})
		c.mediaH.MountBot(a, send)
	})
}

// mountAuthenticated mounts everything that needs a signed-in member.
func (c *components) mountAuthenticated(authed chi.Router) {
	authed.Use(c.sessions.RequireUser, c.perUser.By(auth.UserKey))

	authed.Group(func(j chi.Router) {
		j.Use(httpx.MaxBody(httpx.DefaultMaxBody))
		c.authH.MountAuthenticated(j)
		j.Get("/webrtc/config", webrtcConfig(c.cfg))
		if c.cfg.APIDocs {
			j.Get("/openapi.json", openAPISpec)
		}
		c.chatH.Mount(j)
		c.cmds.MountMember(j)
		c.mediaH.Mount(j)
		c.rolesH.Mount(j)
		c.onboardH.Mount(j)
	})

	// Link cards make outbound requests; they get their own budget.
	if c.previewH != nil {
		authed.Group(func(lp chi.Router) {
			lp.Use(c.previewsPerUser.By(auth.UserKey))
			c.previewH.Mount(lp)
		})
	}

	// Uploads need a larger body ceiling than the JSON default.
	authed.Group(func(u chi.Router) {
		u.Use(httpx.MaxBody(c.mediaH.UploadBodyLimit()), c.uploadsPerUser.By(auth.UserKey))
		c.mediaH.MountUploads(u)
	})

	authed.Route("/admin", c.mountAdmin)
}

func (c *components) mountAdmin(adm chi.Router) {
	adm.Use(auth.RequireAnyPerm(c.permSvc, perm.AdminArea), httpx.MaxBody(httpx.DefaultMaxBody))
	adm.Use(c.auditLog.Middleware)
	c.auditLog.Mount(adm.With(auth.RequirePerm(c.permSvc, perm.ViewAuditLog)))
	c.authH.MountAdmin(adm)
	c.chatH.MountAdmin(adm)
	c.rolesH.MountAdmin(adm)
	c.onboardH.MountAdmin(adm)
	c.webhookH.MountAdmin(adm)
	c.botH.MountAdmin(adm)
	c.mediaH.MountAdmin(adm)
	c.systemH.mountAdmin(adm)
}
