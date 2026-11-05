package forum

import (
	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/chat/mentions"
	"github.com/msch128/mnema-talk/internal/chat/messages"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
)

// API serves forum channels: posts, tags, settings and the unread post list.
// The chat Handler builds it from its own fields, so the server wiring is
// unchanged. Posts go through the messages API, so they obey the same send
// context, automod, slow mode and audit as any message.
type API struct {
	DB     *db.Pool
	Events events.Publisher
	// Guard decides who may see and use a forum channel.
	Guard core.Guard
	// Audit records tag and post changes; nil records nothing.
	Audit *audit.Log
	// Online resolves @here in posts; nil means nobody is online.
	Online mentions.OnlineSource
	// Messages is the messages API of the same handler.
	Messages *messages.API
}
