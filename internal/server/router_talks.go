package server

import (
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat/dm"
	"github.com/msch128/mnema-talk/internal/chat/talks"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// newTalks wires Create-a-Talk lobbies: the hub opens a temporary Talk when a
// member joins a lobby, and the manager follows who is in it through the
// hub's join/leave hook (next to the audit log's).
func (c *components) newTalks() {
	d := c.d
	m := talks.NewTalkManager(&talks.TalkManager{
		DB: d.DB, Events: c.pub, Perms: c.permSvc, Audit: c.auditLog, Voice: c.hub,
		CreateLimit: httpx.NewRateLimiter(talks.TalksPerWindow, talks.TalkWindow).Named("talk_create"),
	})
	if d.Store != nil {
		m.Objects = d.Store
	}
	go m.Run(c.ctx)
	c.hub.Lobby = m
	audited := c.hub.Presence
	calls := dm.NewDMCalls(&dm.DMCalls{DB: d.DB, Events: c.pub, Online: c.chatH.Online, Members: c.hub.VoiceMembers})
	go calls.Run(c.ctx)
	c.hub.Presence = func(user, channel uuid.UUID, joined bool) {
		if audited != nil {
			audited(user, channel, joined)
		}
		m.OnPresence(user, channel, joined)
		calls.OnPresence(user, channel, joined)
	}
	c.chatH.Calls = calls
	c.chatH.Talks = m
	c.chatH.TalkLimit = httpx.NewRateLimiter(talks.TalkRequestsPerMinute, time.Minute).Named("talk")
}
