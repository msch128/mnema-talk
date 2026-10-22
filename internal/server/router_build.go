package server

import (
	"context"
	"net"
	"net/url"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/bot"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/config"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/gateway"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/linkpreview"
	"github.com/msch128/mnema-talk/internal/media"
	"github.com/msch128/mnema-talk/internal/onboarding"
	"github.com/msch128/mnema-talk/internal/opwarn"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/push"
	"github.com/msch128/mnema-talk/internal/roles"
	"github.com/msch128/mnema-talk/internal/ssrf"
	"github.com/msch128/mnema-talk/internal/update"
	"github.com/msch128/mnema-talk/internal/webhook"
	"github.com/msch128/mnema-talk/internal/ws"
)

// The hub ties each WebSocket to its session through this; without it a
// sign-out of one session would not end that session's sockets.
var _ ws.SessionClaimer = (*auth.Sessions)(nil)

// components are the handlers and background workers NewRouter assembles
// before it mounts their routes. The new* methods fill it in stages; a later
// stage may depend on what an earlier one set.
type components struct {
	d       Deps
	cfg     *config.Config
	ctx     context.Context // bounds the background workers
	trusted []*net.IPNet

	sessions *auth.Sessions
	hub      *ws.Hub
	permSvc  *perm.Service
	pub      events.Publisher
	auditLog *audit.Log
	authH    *auth.Handler
	chatH    *chat.Handler
	rolesH   *roles.Handler
	onboardH *onboarding.Handler
	mediaH   *media.Handler

	outgoing *webhook.Dispatcher
	webhookH *webhook.Handler

	pushH *push.Handler

	botH     *bot.Handler
	botAuthn *bot.Authenticator
	cmds     *bot.Commands
	gw       *gateway.Gateway

	systemH *systemHandler
	opsSrc  *opsSource

	previewH *linkpreview.Handler

	wsPerIP, perUser, previewsPerUser, uploadsPerUser *httpx.RateLimiter
}

// newCore builds the session, hub, permission and audit services and the
// chat, role and media handlers on top of them.
func (c *components) newCore() {
	d, cfg := c.d, c.cfg
	c.sessions = &auth.Sessions{
		DB:     d.DB,
		Secret: cfg.JWTSecret,
		TTL:    time.Duration(cfg.SessionExpiryHours) * time.Hour,
		Secure: cfg.SecureCookies(),
	}
	hub := ws.NewHub(d.DB, c.sessions, d.SFU, cfg.AllowedOrigins)
	hub.UpgradeLimit = httpx.NewRateLimiter(30, time.Minute).Named("ws_user")
	c.wsPerIP = httpx.NewRateLimiter(120, time.Minute).Named("ws_ip")
	hub.Version = d.Version
	c.hub = hub
	c.permSvc = perm.NewService(d.DB)
	c.permSvc.SetUploadCeiling(cfg.MaxUploadMB)
	hub.SetPermissions(c.permSvc)
	c.pub = hub
	if d.Events != nil {
		c.pub = d.Events
	}

	c.auditLog = &audit.Log{DB: d.DB, Actor: auditActor, Viewer: auditViewer(c.permSvc)}
	go c.auditLog.RunRetention(c.ctx, cfg.AuditRetentionDays, 6*time.Hour)

	c.authH = auth.NewHandler(c.sessions, c.pub)
	c.authH.Live = hub
	c.authH.Perms = c.permSvc
	c.authH.Audit = c.auditLog
	go c.authH.RunBanExpiry(c.ctx, time.Minute)
	// `mnema-talk unlock` runs in another process and queues requests here.
	go c.authH.RunUnlockWorker(c.ctx, auth.UnlockPollInterval)
	go chat.RunSlowModePrune(c.ctx, d.DB, time.Hour)
	c.chatH = &chat.Handler{DB: d.DB, Events: c.pub, Online: hub, Perms: c.permSvc, Audit: c.auditLog}
	c.rolesH = &roles.Handler{Perms: c.permSvc, Events: c.pub}
	c.onboardH = onboarding.NewHandler(d.DB, c.permSvc)
	if d.Store != nil {
		c.chatH.Objects = d.Store
	}
	c.mediaH = &media.Handler{
		DB:             d.DB,
		Store:          d.Store,
		Bucket:         cfg.S3Bucket,
		Events:         c.pub,
		MaxUploadBytes: int64(cfg.MaxUploadMB) << 20,
		RetentionDays:  cfg.MediaRetentionDays,
		Online:         hub,
		Perms:          c.permSvc,
		Quota: media.Quota{
			PerUserBytes: int64(cfg.UploadQuotaMBPerDay) << 20,
			TotalBytes:   int64(cfg.MediaMaxTotalMB) << 20,
			MinFreeBytes: int64(cfg.MediaMinFreeMB) << 20,
			DiskPath:     cfg.MediaDiskPath,
		},
	}
}

// newWebhooks builds the incoming webhook handler and the outgoing
// dispatcher, which taps the hub's events.
func (c *components) newWebhooks() {
	d := c.d
	// Outgoing webhooks may only reach public addresses (see internal/ssrf).
	ssrfPolicy := &ssrf.Policy{Allow: c.cfg.WebhookAllowPrivateTargets}
	c.outgoing = webhook.NewDispatcher(d.DB, ssrf.NewClient(ssrf.NewDialer(ssrfPolicy), 10*time.Second), d.Version)
	c.hub.Tap = c.outgoing.Observe
	c.hub.Presence = auditPresence(c.ctx, c.auditLog, d.DB)
	go c.outgoing.Run(c.ctx)
	c.webhookH = webhook.New(webhook.Handler{DB: d.DB, Events: c.pub, Media: c.mediaH, Perms: c.permSvc, Online: c.hub, PublicURL: c.cfg.PublicURL, Outgoing: c.outgoing, Policy: ssrfPolicy})
}

// newBots builds the bot API, its commands and the gateway, and wires the
// gateway into the hub and the permission service.
func (c *components) newBots() {
	d, hub := c.d, c.hub
	c.botH = &bot.Handler{DB: d.DB, Perms: c.permSvc, Media: c.mediaH, Events: c.pub}
	c.botAuthn = bot.NewAuthenticator(d.DB)
	c.botAuthn.Perms = c.permSvc
	c.cmds = bot.NewCommands(bot.Commands{DB: d.DB, Perms: c.permSvc, Events: c.pub, Online: hub})
	gw := gateway.New(gateway.Config{Auth: c.botAuthn, Perms: c.permSvc, DB: d.DB, PublicURL: c.cfg.PublicURL})
	c.gw = gw
	c.botH.OnRevoke = gw.Revoke
	gw.OnPresence = hub.SetBotPresence
	gw.OnStatus = func(id uuid.UUID, status string, a *gateway.Activity) {
		var act *ws.BotActivity
		if a != nil {
			act = &ws.BotActivity{Type: a.Type, Name: a.Name}
		}
		hub.SetBotStatus(id, status, act)
	}
	c.authH.OnAccountEnded = gw.RevokeUser
	c.cmds.Deliver = gw
	go gw.Run(c.ctx)
	// Both the hub and the gateway learn about permission changes, and the
	// gateway sees every event the hub publishes (as outgoing webhooks do).
	c.permSvc.SetNotifier(notifiers{hub, gw})
	prevTap := hub.Tap
	hub.Tap = func(ch uuid.UUID, eventType string, payload any) {
		if prevTap != nil {
			prevTap(ch, eventType, payload)
		}
		gw.Observe(ch, eventType, payload)
	}
}

// newPush builds Web Push: the key pair, the sender, the handler for the
// devices API and, when push is on, the delivery service that taps the hub's
// events. Requests go only to push services, through the SSRF-guarded client
// that allows public addresses alone.
func (c *components) newPush() error {
	cfg := c.cfg
	h := push.Handler{DB: c.d.DB, Sessions: c.sessions, Enabled: cfg.PushEnabled, PreviewsAllowed: cfg.PushPreviews == "user"}
	if cfg.PushEnabled {
		keys, err := push.LoadKeys(c.ctx, c.d.DB)
		if err != nil {
			return err
		}
		client := c.d.PushClient
		if client == nil {
			client = ssrf.NewClient(ssrf.NewDialer(&ssrf.Policy{}), 10*time.Second)
		}
		svc := &push.Service{
			DB: c.d.DB, Perms: c.permSvc, Previews: h.PreviewsAllowed, Burst: c.d.PushBurst,
			Sender: &push.Sender{Keys: keys, Subject: push.Subject(cfg.PushVAPIDSubject, cfg.PublicURL), HTTP: client},
		}
		prevTap := c.hub.Tap
		c.hub.Tap = func(ch uuid.UUID, eventType string, payload any) {
			if prevTap != nil {
				prevTap(ch, eventType, payload)
			}
			svc.Observe(ch, eventType, payload)
		}
		go svc.Run(c.ctx)
		h.Keys, h.Service = keys, svc
	}
	c.pushH = push.NewHandler(h)
	return nil
}

// newSystem builds the admin System tab's handler, its ops sampler and, when
// configured, the self-update client.
func (c *components) newSystem() error {
	d, cfg := c.d, c.cfg
	h := &systemHandler{
		cfg:      cfg,
		db:       d.DB,
		hub:      c.hub,
		sfu:      d.SFU,
		storage:  d.StorageReady,
		hasStore: d.Store != nil,
		version:  d.Version,
		started:  time.Now(),
		updates:  d.Updates,
	}
	if h.version == "" {
		h.version = "dev"
	}
	h.events = c.pub
	h.warnings = opwarn.Default
	c.opsSrc = &opsSource{db: d.DB, hub: c.hub, sfu: d.SFU, storage: d.StorageReady, hasStore: d.Store != nil, version: h.version}
	h.ops = newOpsSampler(c.opsSrc)
	go h.ops.run(c.ctx)
	h.self = selfUpdate{confirm: c.authH.ConfirmPassword, now: time.Now}
	if cfg.SelfUpdateConfigured() {
		u, err := update.NewUpdater(cfg.UpdaterURL, cfg.UpdaterToken)
		if err != nil {
			return err
		}
		h.self.updater = u
	}
	c.systemH = h
	return nil
}

// newLinkPreviews builds the link card handler when previews are enabled.
func (c *components) newLinkPreviews() {
	if !c.cfg.LinkPreviews {
		return
	}
	fetcher := linkpreview.New()
	// Our own public address leads back into the LAN through the
	// router (hairpin NAT); never let a preview fetch go there.
	if u, err := url.Parse(c.cfg.PublicURL); err == nil {
		hosts := append([]string{u.Hostname()}, c.cfg.WebRTCAnnounce...)
		deny := func() {
			dctx, dcancel := context.WithTimeout(c.ctx, denyTimeout)
			defer dcancel()
			fetcher.Deny(dctx, hosts...)
		}
		deny()
		// A home connection's public IP changes; keep the list current.
		go refreshPreviewDeny(c.ctx, denyRefresh, deny)
	}
	c.previewH = &linkpreview.Handler{Fetcher: fetcher}
}
