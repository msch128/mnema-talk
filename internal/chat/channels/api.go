package channels

import (
	"github.com/go-chi/chi/v5"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// API serves categories, channels, their layout and auto-delete. The chat
// Handler builds it from its own fields, so the server wiring is unchanged; the
// bot API reuses ListChannels.
type API struct {
	DB     *db.Pool
	Events events.Publisher
	// Guard decides who may see which channel and category. Without Perms the
	// admin routes stay Owner-only.
	Guard core.Guard
	// Objects deletes the media of removed channels; nil skips it.
	Objects core.ObjectDeleter
	// Voice empties the voice room of a deleted channel; nil skips it.
	Voice core.VoiceRooms
}

// Mount registers the member-facing channel routes (RequireUser must already
// apply).
func (a *API) Mount(r chi.Router) {
	r.Get("/channels", httpx.Handle(a.ListChannels))
	r.Get("/message-settings", httpx.Handle(a.ServeLifetimeSettings))
}

// MountAdmin registers channel and category management (the Manage channels
// permission applies).
func (a *API) MountAdmin(r chi.Router) {
	r.With(auth.RequirePerm(a.Guard.Perms, perm.ManageRoles)).Get("/roles/{id}/view-as", httpx.Handle(a.ViewAsRole))
	r.Group(func(r chi.Router) {
		r.Use(auth.RequirePerm(a.Guard.Perms, perm.Administrator))
		r.Get("/message-settings", httpx.Handle(a.ServeMessageSettings))
		r.Put("/message-settings/lifetimes", httpx.Handle(a.ServeSetLifetimeSettings))
		r.Put("/message-settings/auto-delete", httpx.Handle(a.ServeSetAutoDeleteDefault))
		r.Post("/message-settings/auto-delete/preview", httpx.Handle(a.ServePreviewAutoDeleteDefault))
	})
	r = r.With(auth.RequirePerm(a.Guard.Perms, perm.ManageChannels))
	r.Post("/categories", httpx.Handle(a.ServeCreateCategory))
	r.Delete("/categories/{id}", httpx.Handle(a.ServeDeleteCategory))
	r.Post("/channels", httpx.Handle(a.CreateChannel))
	r.Delete("/channels/{id}", httpx.Handle(a.ServeDeleteChannel))
	r.Patch("/channels/{id}", httpx.Handle(a.UpdateChannel))
	r.Put("/channels/{id}/auto-delete", httpx.Handle(a.ServeSetAutoDelete))
	r.Post("/channels/{id}/auto-delete/preview", httpx.Handle(a.ServePreviewAutoDelete))
	r.Post("/channels/{id}/duplicate", httpx.Handle(a.ServeDuplicateChannel))
	r.Patch("/categories/{id}", httpx.Handle(a.ServeRenameCategory))
	r.Put("/layout", httpx.Handle(a.ServeApplyLayout))
}
