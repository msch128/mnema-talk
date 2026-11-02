package server

import (
	"slices"
	"time"

	"github.com/msch128/mnema-talk/internal/automod"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/communityevents"
	"github.com/msch128/mnema-talk/internal/discordimport"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/later"
	"github.com/msch128/mnema-talk/internal/moderation"
	"github.com/msch128/mnema-talk/internal/poll"
	"github.com/msch128/mnema-talk/internal/push"
)

// newChat builds the chat handler with its rate limits and the background
// jobs that belong to messages: edit-history retention, poll folding and the
// per-channel auto-delete.
func (c *components) newChat() {
	d, cfg, hub := c.d, c.cfg, c.hub
	c.chatH = &chat.Handler{
		DB: d.DB, Events: c.pub, Online: hub, Perms: c.permSvc, Audit: c.auditLog,
		EditHistoryLimit: httpx.NewRateLimiter(chat.EditHistoryRate, time.Minute).Named("edit_history"),
		PinLimit:         httpx.NewRateLimiter(chat.PinChangesPerMinute, time.Minute).Named("pin"),
		FollowLimit:      httpx.NewRateLimiter(chat.ThreadFollowChangesPerMinute, time.Minute).Named("thread_follow"),
		BlockLimit:       httpx.NewRateLimiter(chat.BlockChangesPerMinute, time.Minute).Named("block"),
		ReportLimit:      httpx.NewRateLimiter(chat.ReportsPerHour, time.Hour).Named("report"),
		SavedLimit:       httpx.NewRateLimiter(chat.SavedChangesPerMinute, time.Minute).Named("saved"),
		ForwardLimit:     httpx.NewRateLimiter(chat.ForwardsPerMinute, time.Minute).Named("forward"),
		DMLimit:          httpx.NewRateLimiter(chat.NewDMsPerMinute, time.Minute).Named("dm_start"),
	}
	go chat.RunEditHistoryRetention(c.ctx, d.DB, cfg.EditHistoryRetentionDays, 6*time.Hour)
	go poll.RunFold(c.ctx, d.DB, 10*time.Minute)
	autoDelete := &chat.AutoDeleter{DB: d.DB, Events: c.pub, Audit: c.auditLog}
	if d.Store != nil {
		autoDelete.Objects = d.Store
	}
	go autoDelete.Run(c.ctx, chat.AutoDeleteEvery)
}

// newImport builds the Discord structure import.
func (c *components) newImport() {
	d, hub := c.d, c.hub
	c.importH = &discordimport.Handler{
		Perms: c.permSvc, Limit: httpx.NewRateLimiter(discordimport.ImportsPerMinute, time.Minute).Named("discord_import"),
		Importer: &discordimport.Importer{
			DB: d.DB, Perms: c.permSvc, Events: c.pub, Audit: c.auditLog, Voice: hub,
			Features: func(name string) bool { return slices.Contains(Features, name) },
		},
	}
}

// newModeration builds the automod engine, makes the message and sign-up paths
// use it and creates the handler of the moderators' routes.
func (c *components) newModeration() {
	c.engine = automod.New(c.d.DB, c.permSvc, c.pub, c.auditLog)
	automod.Attach(c.d.DB, c.engine)
	c.modH = &moderation.Handler{DB: c.d.DB, Perms: c.permSvc, Engine: c.engine}
}

// newLater builds reminders and scheduled messages; pusher is nil when Web
// Push is off.
func (c *components) newLater(pusher later.Notifier) {
	c.later = later.NewService(later.Service{DB: c.d.DB, Perms: c.permSvc, Events: c.pub, Online: c.hub, Push: pusher})
	go c.later.Run(c.ctx)
}

func (c *components) newEvents() {
	var pushSvc *push.Service
	if c.pushH != nil {
		pushSvc = c.pushH.Service
	}
	c.eventsH = &communityevents.Handler{
		DB:           c.d.DB,
		Store:        communityevents.NewStore(c.d.DB),
		MediaStore:   c.d.Store,
		MediaBucket:  c.cfg.S3Bucket,
		Events:       c.pub,
		Perms:        c.permSvc,
		Audit:        c.auditLog,
		Push:         pushSvc,
		CommentLimit: httpx.NewRateLimiter(communityevents.CommentsPerMinute, time.Minute).Named("event_comment"),
	}
	c.eventsScheduler = communityevents.NewScheduler(c.eventsH)
	if c.d.DB != nil { // router-only tests run without a database
		c.eventsScheduler.Start(c.ctx)
	}
}
