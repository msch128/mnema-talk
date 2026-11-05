package readstate

import (
	"github.com/go-chi/chi/v5"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// API serves the read markers and the notification settings of a member. The
// chat Handler builds it from its own fields, so wiring in the server does not
// change.
type API struct {
	DB     *db.Pool
	Events events.Publisher
	// Guard decides who may see which channel and category.
	Guard core.Guard
}

// Mount registers the read-state and notification routes (RequireUser must
// already apply).
func (a API) Mount(r chi.Router) {
	r.Get("/read-state", httpx.Handle(a.readState))
	r.Post("/read-state/read-all", httpx.Handle(a.markAllRead))
	r.Post("/channels/{channelID}/read", httpx.Handle(a.markRead))
	r.Post("/channels/{channelID}/unread", httpx.Handle(a.markUnread))
	r.Get("/channels/{channelID}/first-unread", httpx.Handle(a.firstUnread))
	r.Put("/channels/{channelID}/notifications", httpx.Handle(a.setNotifyLevel))
	r.Put("/categories/{id}/notifications", httpx.Handle(a.setCategoryNotify))
	r.Get("/notification-settings", httpx.Handle(a.notificationSettings))
	r.Put("/notification-settings/server", httpx.Handle(a.setServerNotify))
}
