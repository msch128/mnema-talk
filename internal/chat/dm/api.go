package dm

import (
	"github.com/go-chi/chi/v5"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// API serves direct messages: the conversation list, starting one, members,
// privacy and the per-member preferences. The chat Handler builds it from its
// own fields, so the server wiring is unchanged.
type API struct {
	DB     *db.Pool
	Events events.Publisher
	// Guard decides who may use direct messages (Send direct messages).
	Guard core.Guard
	// Objects deletes the media of a conversation that ends; nil skips it.
	Objects core.ObjectDeleter
	// Voice ends the call of a conversation that ends; nil skips it.
	Voice core.VoiceRooms
	// Calls rings the members of a conversation; nil means no ringing.
	Calls *DMCalls
	// DMLimit caps starting direct messages and adding members to them per
	// member; nil means no cap.
	DMLimit *httpx.RateLimiter
}

// Mount registers the direct message routes (RequireUser must already apply).
func (a *API) Mount(r chi.Router) {
	a.mountDMs(r)
}
