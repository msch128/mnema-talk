package saved

import (
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// API serves a member's saved messages. The chat Handler builds it from its
// own fields, so the server wiring is unchanged.
type API struct {
	DB     *db.Pool
	Events events.Publisher
	// Guard limits the list to channels the caller may view.
	Guard core.Guard
	// SavedLimit caps saving and removing saved messages per member; nil means no cap.
	SavedLimit *httpx.RateLimiter
}
