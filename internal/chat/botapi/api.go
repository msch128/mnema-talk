package botapi

import (
	"net/http"

	"github.com/msch128/mnema-talk/internal/chat/channels"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/chat/messages"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
)

// API serves the Discord-style bot REST routes. They reuse the member handlers
// of the messages and channels APIs, so the permission checks are the same
// ones, decided for the bot's roles and overwrites. The chat Handler builds it
// from its own fields, so the server wiring is unchanged.
type API struct {
	DB     *db.Pool
	Events events.Publisher
	// Guard decides what the bot may see and do.
	Guard core.Guard
	// Messages and Channels are the APIs of the same handler. A bot's message
	// is posted by Messages.Posts.
	Messages *messages.API
	Channels *channels.API

	// send is the per-channel post limit MountBot was given; an edit that may
	// notify counts against it.
	send func(http.Handler) http.Handler
}
