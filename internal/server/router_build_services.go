package server

import (
	"context"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/auth/account"
	"github.com/msch128/mnema-talk/internal/auth/authn"
	"github.com/msch128/mnema-talk/internal/auth/memberadmin"
	"github.com/msch128/mnema-talk/internal/automod"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/chat/blocks"
	"github.com/msch128/mnema-talk/internal/chat/channels"
	"github.com/msch128/mnema-talk/internal/chat/dm"
	"github.com/msch128/mnema-talk/internal/chat/messages"
	"github.com/msch128/mnema-talk/internal/chat/reports"
	"github.com/msch128/mnema-talk/internal/chat/saved"
	"github.com/msch128/mnema-talk/internal/communityevents"
	"github.com/msch128/mnema-talk/internal/discordimport"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/later"
	"github.com/msch128/mnema-talk/internal/moderation"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/poll"
	"github.com/msch128/mnema-talk/internal/push"
	"github.com/msch128/mnema-talk/internal/wiki"
	"github.com/msch128/mnema-talk/internal/ws"
)

// newChat builds the chat handler with its rate limits and the background
// jobs that belong to messages: edit-history retention, poll folding and the
// per-channel auto-delete.
func (c *components) newChat() {
	d, cfg, hub := c.d, c.cfg, c.hub
	c.chatH = &chat.Handler{
		DB: d.DB, Events: c.pub, Online: hub, Perms: c.permSvc, Audit: c.auditLog,
		EditHistoryLimit: httpx.NewRateLimiter(messages.EditHistoryRate, time.Minute).Named("edit_history"),
		PinLimit:         httpx.NewRateLimiter(messages.PinChangesPerMinute, time.Minute).Named("pin"),
		FollowLimit:      httpx.NewRateLimiter(messages.ThreadFollowChangesPerMinute, time.Minute).Named("thread_follow"),
		BlockLimit:       httpx.NewRateLimiter(blocks.BlockChangesPerMinute, time.Minute).Named("block"),
		ReportLimit:      httpx.NewRateLimiter(reports.ReportsPerHour, time.Hour).Named("report"),
		SavedLimit:       httpx.NewRateLimiter(saved.SavedChangesPerMinute, time.Minute).Named("saved"),
		ForwardLimit:     httpx.NewRateLimiter(messages.ForwardsPerMinute, time.Minute).Named("forward"),
		DMLimit:          httpx.NewRateLimiter(dm.NewDMsPerMinute, time.Minute).Named("dm_start"),
	}
	go messages.RunEditHistoryRetention(c.ctx, d.DB, cfg.EditHistoryRetentionDays, 6*time.Hour)
	go poll.RunFold(c.ctx, d.DB, 10*time.Minute)
	autoDelete := &channels.AutoDeleter{DB: d.DB, Events: c.pub, Audit: c.auditLog}
	if d.Store != nil {
		autoDelete.Objects = d.Store
	}
	go autoDelete.Run(c.ctx, channels.AutoDeleteEvery)
}

// newImport builds the Discord structure import.
func (c *components) newImport() {
	d, hub := c.d, c.hub
	c.importH = discordimport.New(discordimport.Handler{
		Perms: c.permSvc, Limit: httpx.NewRateLimiter(discordimport.ImportsPerMinute, time.Minute).Named("discord_import"),
		Importer: &discordimport.Importer{
			DB: d.DB, Perms: c.permSvc, Events: c.pub, Audit: c.auditLog, Voice: hub,
			Features: func(name string) bool { return slices.Contains(Features, name) },
		},
	})
}

// newModeration builds the automod engine, makes the message and sign-up paths
// use it and creates the handler of the moderators' routes.
func (c *components) newModeration() {
	c.engine = automod.New(c.d.DB, c.permSvc, c.pub, c.auditLog)
	automod.Attach(c.d.DB, c.engine)
	c.modH = moderation.New(moderation.Handler{DB: c.d.DB, Perms: c.permSvc, Engine: c.engine})
}

// newLater builds reminders and scheduled messages; pusher is nil when Web
// Push is off.
func (c *components) newLater(pusher later.Notifier) {
	c.later = later.NewService(later.Service{DB: c.d.DB, Perms: c.permSvc, Events: c.pub, Online: c.hub, Push: pusher})
	go c.later.Run(c.ctx)
}

// newWiki builds the wiki handler; its picture uploads reserve quota through
// the media handler, so newCore must have run.
func (c *components) newWiki() {
	c.wikiH = wiki.New(wiki.Handler{
		Store:       wiki.NewStore(c.d.DB),
		MediaStore:  c.d.Store,
		MediaBucket: c.cfg.S3Bucket,
		Reserve:     c.mediaH.ReserveUpload,
		Perms:       c.permSvc,
		Audit:       c.auditLog,
		WriteLimit:  httpx.NewRateLimiter(wiki.EditsPerMinute, time.Minute).Named("wiki"),
	})
}

// newEvents builds the community events handler and starts its scheduler.
// It takes the push service from newPush, so it runs after it.
func (c *components) newEvents() {
	var pushSvc *push.Service
	if c.pushH != nil {
		pushSvc = c.pushH.Service
	}
	c.eventsH = communityevents.New(&communityevents.Handler{
		DB:           c.d.DB,
		Store:        communityevents.NewStore(c.d.DB),
		MediaStore:   c.d.Store,
		MediaBucket:  c.cfg.S3Bucket,
		Events:       c.pub,
		Perms:        c.permSvc,
		Audit:        c.auditLog,
		Push:         pushSvc,
		CommentLimit: httpx.NewRateLimiter(communityevents.CommentsPerMinute, time.Minute).Named("event_comment"),
	})
	c.eventsScheduler = communityevents.NewScheduler(c.eventsH)
	if c.d.DB != nil { // router-only tests run without a database
		c.eventsScheduler.Start(c.ctx)
	}
}

// newAuth builds the sign-in handler and the route sets split out of package
// auth, and runs the account sweeps that live as long as the server: ban and
// status expiry, the session sweep and the unlock worker.
func (c *components) newAuth(hub *ws.Hub) {
	c.authH = authn.NewHandler(c.sessions, c.permSvc, c.pub)
	c.authH.Live = hub
	c.authH.Names = c.d.Names
	c.authH.HiddenMessages = c.hiddenMessageCount
	c.authH.Audit = c.auditLog
	c.authH.Lifetime = c.ctx
	c.newAuthAPIs()
	go c.members.RunBanExpiry(c.ctx, time.Minute)
	go c.authH.RunStatusExpiry(c.ctx, 0)
	go c.authH.RunSessionSweep(c.ctx, auth.DefaultSessionSweepEvery)
	// `mnema-talk unlock` runs in another process and queues requests here.
	go c.authH.RunUnlockWorker(c.ctx, authn.UnlockPollInterval)
}

// newAuthAPIs builds the route sets split out of package auth: the member-admin
// routes and the account export and deletion routes.
func (c *components) newAuthAPIs() {
	c.members = memberadmin.New(c.authH)
	c.account = account.New(c.authH)
}

// tapThreadFollows tells open clients which threads they follow after a reply
// made them follow it (see chat.NewFollowSync).
func (c *components) tapThreadFollows() {
	sync := messages.NewFollowSync(c.ctx, c.d.DB, c.permSvc, c.pub)
	c.addTap(sync)
	c.addDMTap(sync) // a call's chat has threads too; the update goes to the followers only
}

// hiddenMessageCount counts the messages one author has in channels the viewer
// cannot see (a profile subtracts them from the total).
func (c *components) hiddenMessageCount(ctx context.Context, viewer perm.Subject, author uuid.UUID) (int64, error) {
	snap, err := c.permSvc.Snapshot(ctx)
	if err != nil {
		return 0, err
	}
	hidden := snap.HiddenChannels(viewer)
	if len(hidden) == 0 {
		return 0, nil
	}
	return chat.MessageCountByAuthor(ctx, c.d.DB, author, hidden)
}

// addDMTap is addTap for the events of conversations and their Talks, which
// reach no webhook or bot.
func (c *components) addDMTap(f func(ch uuid.UUID, eventType string, payload any)) {
	prev := c.hub.DMTap
	c.hub.DMTap = func(ch uuid.UUID, eventType string, payload any) {
		if prev != nil {
			prev(ch, eventType, payload)
		}
		f(ch, eventType, payload)
	}
}
