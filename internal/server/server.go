// Package server wires configuration, storage and feature handlers into the
// HTTP router. cmd/server only loads config and calls Run.
package server

import (
	"context"
	"net"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/automod"
	"github.com/msch128/mnema-talk/internal/bot"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/config"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/gateway"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/media"
	"github.com/msch128/mnema-talk/internal/push"
	"github.com/msch128/mnema-talk/internal/sfu"
	"github.com/msch128/mnema-talk/internal/update"
	"github.com/msch128/mnema-talk/internal/webhook"
	"github.com/msch128/mnema-talk/internal/ws"
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
	// PushClient replaces the SSRF-guarded client that posts Web Push
	// requests (tests point it at a local push service); PushBurst replaces
	// the five second window that merges a burst of messages into one push.
	PushClient push.HTTPClient
	PushBurst  time.Duration
}

// SystemAdminMounter mounts the admin system routes.
type SystemAdminMounter interface {
	MountAdmin(r chi.Router)
}

// PreviewDeps holds internal dependencies needed by the native preview router.
type PreviewDeps struct {
	Accounts *auth.Handler
	Chat     *chat.Handler
	System   SystemAdminMounter
	PerUser  *httpx.RateLimiter
	Trusted  []*net.IPNet
}

// Router holds the assembled handler and the hub (for shutdown/tests).
type Router struct {
	// preview is retained for explicitly constructed native preview; NewRouter mounts no native routes.
	preview PreviewDeps
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

// PreviewDeps returns internal dependencies needed by the native preview router.
func (r *Router) PreviewDeps() PreviewDeps {
	return r.preview
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

	c := &components{d: d, cfg: cfg, ctx: ctx, trusted: trusted}
	c.newCore()
	if err := c.newMail(); err != nil {
		cancel()
		return nil, err
	}
	c.newWebhooks()
	c.newTalks()
	c.newBots()
	c.tapThreadFollows()
	if err := c.newPush(); err != nil {
		cancel()
		return nil, err
	}
	if err := c.newSystem(); err != nil {
		cancel()
		return nil, err
	}
	c.newLinkPreviews()
	c.perUser = httpx.NewRateLimiter(600, time.Minute).Named("api_user")
	c.previewsPerUser = httpx.NewRateLimiter(120, time.Minute).Named("link_preview")
	c.uploadsPerUser = httpx.NewRateLimiter(30, time.Minute).Named("upload")
	c.newModeration()

	return &Router{
		Handler:  c.buildHandler(),
		Hub:      c.hub,
		Webhooks: c.webhookH,
		Bots:     c.botAuthn,
		Gateway:  c.gw,
		cancel:   func() { automod.Detach(d.DB, c.engine); cancel() },
		outgoing: c.outgoing,
		preview:  PreviewDeps{Accounts: c.authH, Chat: c.chatH, System: c.systemH, PerUser: c.perUser, Trusted: trusted},
	}, nil
}
