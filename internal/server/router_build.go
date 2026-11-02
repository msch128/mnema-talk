package server

import (
	"context"
	"errors"
	"net"
	"net/url"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/automod"
	"github.com/msch128/mnema-talk/internal/bot"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/communityevents"
	"github.com/msch128/mnema-talk/internal/config"
	"github.com/msch128/mnema-talk/internal/discordimport"
	"github.com/msch128/mnema-talk/internal/emoji"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/gateway"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/later"
	"github.com/msch128/mnema-talk/internal/linkpreview"
	"github.com/msch128/mnema-talk/internal/mailer"
	"github.com/msch128/mnema-talk/internal/media"
	"github.com/msch128/mnema-talk/internal/moderation"
	"github.com/msch128/mnema-talk/internal/onboarding"
	"github.com/msch128/mnema-talk/internal/opwarn"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/poll"
	"github.com/msch128/mnema-talk/internal/push"
	"github.com/msch128/mnema-talk/internal/roles"
	"github.com/msch128/mnema-talk/internal/slash"
	"github.com/msch128/mnema-talk/internal/slashcmd"
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
const (
	// apiWritesPerMinute is a member's budget for requests that change
	// something (and for anything else that is not a read).
	apiWritesPerMinute = 600
	// apiReadsPerMinute is the separate budget for GET, HEAD and OPTIONS: opening
	// and closing posts, scrolling and switching channels fetch many pages a
	// second, and none of it may be refused. 50 a second still stops a runaway
	// script.
	apiReadsPerMinute = 3000
)

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
	importH  *discordimport.Handler
	onboardH *onboarding.Handler
	mediaH   *media.Handler
	emojiH   *emoji.Handler
	modH     *moderation.Handler
	engine   *automod.Engine

	outgoing *webhook.Dispatcher
	webhookH *webhook.Handler

	pushH           *push.Handler
	later           *later.Service
	eventsH         *communityevents.Handler
	eventsScheduler *communityevents.Scheduler

	botH     *bot.Handler
	botAuthn *bot.Authenticator
	cmds     *bot.Commands
	pollSvc  *poll.Service
	gw       *gateway.Gateway

	mail    *mailer.Mailer // nil: SMTP not configured
	systemH *systemHandler
	opsSrc  *opsSource

	previewH *linkpreview.Handler

	wsPerIP, perUser, readsPerUser, previewsPerUser, uploadsPerUser *httpx.RateLimiter
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
	hub.UpgradeSeen = func(upgraded bool) { d.Setup.RecordWebSocket(upgraded, time.Now()) }
	c.hub = hub
	c.permSvc = d.Perms
	if c.permSvc == nil {
		c.permSvc = perm.NewService(d.DB)
	}
	c.permSvc.SetUploadCeiling(cfg.MaxUploadMB)
	hub.SetPermissions(c.permSvc)
	if d.TURN != nil {
		d.TURN.SetRelayCheck(c.mayJoinVoice)
	}
	c.pub = hub
	if d.Events != nil {
		c.pub = d.Events
	}

	c.auditLog = &audit.Log{DB: d.DB, Actor: auditActor, Viewer: auditViewer(c.permSvc)}
	go c.auditLog.RunRetention(c.ctx, cfg.AuditRetentionDays, 6*time.Hour)

	c.authH = auth.NewHandler(c.sessions, c.pub)
	c.authH.Live = hub
	c.authH.Names = d.Names
	c.authH.Perms = c.permSvc
	c.authH.Audit = c.auditLog
	c.authH.Lifetime = c.ctx
	go c.authH.RunBanExpiry(c.ctx, time.Minute)
	go c.authH.RunStatusExpiry(c.ctx, 0)
	go c.authH.RunSessionSweep(c.ctx, auth.DefaultSessionSweepEvery)
	// `mnema-talk unlock` runs in another process and queues requests here.
	go c.authH.RunUnlockWorker(c.ctx, auth.UnlockPollInterval)
	if d.Store != nil {
		c.authH.Objects = d.Store
		c.authH.ObjectMissing = media.IsMissingObject
		go c.authH.RunExportSweep(c.ctx, 0)
	}
	go chat.RunSlowModePrune(c.ctx, d.DB, time.Hour)
	c.newChat()
	c.rolesH = &roles.Handler{Perms: c.permSvc, Events: c.pub, Audit: c.auditLog, SelfRoleLimit: httpx.NewRateLimiter(30, time.Minute).Named("self_roles")}
	c.onboardH = onboarding.NewHandler(d.DB, c.permSvc)
	c.onboardH.Names, c.onboardH.Events = d.Names, c.pub
	c.newImport()
	if d.Store != nil {
		c.onboardH.Store = d.Store
		c.chatH.Objects = d.Store
		c.importH.Importer.Objects = d.Store
	}
	c.emojiH = &emoji.Handler{DB: d.DB, Store: d.Store, Events: c.pub, Perms: c.permSvc, Max: cfg.EmojiMax,
		UploadLimit: httpx.NewRateLimiter(emoji.UploadsPer10Minutes, 10*time.Minute).Named("emoji_upload")}
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
	media.StartAvatarReencode(c.ctx, d.DB, d.Store, cfg.S3Bucket, &audit.Log{DB: d.DB}, c.pub)
}

// newMail builds the SMTP client when configured and hands it to the account
// flows that need it. Without SMTP_HOST both stay nil and those flows are off.
func (c *components) newMail() error {
	m, err := mailer.New(c.cfg.SMTP)
	if err != nil {
		return err
	}
	c.authH.PublicURL = c.cfg.PublicURL
	if m != nil {
		c.mail = m
		c.authH.Mail = m
	}
	return nil
}

// newWebhooks builds the incoming webhook handler and the outgoing
// dispatcher, which taps the hub's events.
func (c *components) newWebhooks() {
	d := c.d
	// Outgoing webhooks may only reach public addresses (see internal/ssrf).
	ssrfPolicy := &ssrf.Policy{Allow: c.cfg.WebhookAllowPrivateTargets}
	c.outgoing = webhook.NewDispatcher(d.DB, ssrf.NewClient(ssrf.NewDialer(ssrfPolicy), 10*time.Second), d.Version)
	c.addTap(c.outgoing.Observe)
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
	c.pollSvc = poll.NewService(poll.Service{DB: d.DB, Perms: c.permSvc, Events: c.pub, Online: hub, Audit: c.auditLog})
	c.cmds = bot.NewCommands(bot.Commands{DB: d.DB, Perms: c.permSvc, Events: c.pub, Online: hub, Builtins: builtinCommands(c.pollSvc, slashcmd.Services{DB: d.DB, Perms: c.permSvc, Events: c.pub, Online: hub, Auth: c.authH, Chat: c.chatH, Audit: c.auditLog})})
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
	c.permSvc.SetNotifier(notifiers{hub, gw, eventsNotifier{c}})
	c.addTap(gw.Observe)
}

// addTap chains f after the taps already on the hub, so no component replaces
// another's tap.
func (c *components) addTap(f func(ch uuid.UUID, eventType string, payload any)) {
	prev := c.hub.Tap
	c.hub.Tap = func(ch uuid.UUID, eventType string, payload any) {
		if prev != nil {
			prev(ch, eventType, payload)
		}
		f(ch, eventType, payload)
	}
}

// tapThreadFollows tells open clients which threads they follow after a reply
// made them follow it (see chat.NewFollowSync).
func (c *components) tapThreadFollows() {
	c.addTap(chat.NewFollowSync(c.ctx, c.d.DB, c.permSvc, c.pub))
}

// newPush builds Web Push: the key pair, the sender, the handler for the
// devices API and, when push is on, the delivery service that taps the hub's
// events. Requests go only to push services, through the SSRF-guarded client
// that allows public addresses alone.
func (c *components) newPush() error {
	cfg := c.cfg
	var pusher later.Notifier
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
			DB: c.d.DB, Names: c.d.Names, Perms: c.permSvc, Previews: h.PreviewsAllowed, Burst: c.d.PushBurst,
			Sender: &push.Sender{Keys: keys, Subject: push.Subject(cfg.PushVAPIDSubject, cfg.PublicURL), HTTP: client},
		}
		c.addTap(svc.Observe)
		c.hub.DMTap = svc.Observe // conversations reach no webhook or bot, only push to their members
		go svc.Run(c.ctx)
		h.Keys, h.Service = keys, svc
		pusher = svc
	}
	c.pushH = push.NewHandler(h)
	c.newLater(pusher)
	return nil
}

// mayJoinVoice is what the built-in TURN relay asks before it hands out or
// honours credentials: the account is neither disabled nor banned and the
// member could join a Talk right now. An error means the answer is unknown
// (a database or permission lookup failed), which the relay must not treat as
// a withdrawal.
func (c *components) mayJoinVoice(user uuid.UUID) (bool, error) {
	ctx, cancel := context.WithTimeout(c.ctx, 2*time.Second)
	defer cancel()
	if c.d.DB != nil {
		var active bool
		if err := c.d.DB.QueryRow(ctx, `SELECT NOT `+auth.InactiveSQL("")+` FROM users WHERE id = $1`, user).Scan(&active); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return false, nil
			}
			return false, err
		}
		if !active {
			return false, nil
		}
	}
	snap, err := c.permSvc.Snapshot(ctx)
	if err != nil {
		return false, err
	}
	return snap.MayConnectAnywhere(perm.Subject{ID: user}) && !snap.RulesPending(user), nil
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
		turn:     d.TURN,
		storage:  d.StorageReady,
		hasStore: d.Store != nil,
		version:  d.Version,
		names:    d.Names,
		started:  time.Now(),
		updates:  d.Updates,
		mail:     c.mail,

		testMailLimit: httpx.NewRateLimiter(5, 10*time.Minute).Named("test_mail"),
		probeLimit:    httpx.NewRateLimiter(12, 10*time.Minute).Named("connection_probe"),
		wsReportLimit: httpx.NewRateLimiter(30, 10*time.Minute).Named("ws_report"),
		speed:         newSpeedTest(),
	}
	if h.version == "" {
		h.version = "dev"
	}
	h.events = c.pub
	h.speed.callRunning = func() bool { return c.hub != nil && c.hub.Stats().VoiceParticipants > 0 }
	h.warnings = opwarn.Default
	h.setup = d.Setup
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

// builtinCommands is the server's own slash commands, in menu order. Adding a
// command means implementing slash.Builtin and listing it here.
func builtinCommands(polls *poll.Service, svc slashcmd.Services) *slash.Registry {
	reg := slash.NewRegistry()
	for _, b := range append([]slash.Builtin{poll.Command{Service: polls}}, slashcmd.Commands(svc)...) {
		if err := reg.Add(b); err != nil {
			panic(err) // a malformed built-in definition is a programming error
		}
	}
	return reg
}
