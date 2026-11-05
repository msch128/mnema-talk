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
}

// MountAdmin registers channel and category management (the Manage channels
// permission applies).
func (a *API) MountAdmin(r chi.Router) {
	r.With(auth.RequirePerm(a.Guard.Perms, perm.ManageRoles)).Get("/roles/{id}/view-as", httpx.Handle(a.ViewAsRole))
	r = r.With(auth.RequirePerm(a.Guard.Perms, perm.ManageChannels))
	r.Post("/categories", httpx.Handle(a.CreateCategory))
	r.Delete("/categories/{id}", httpx.Handle(a.DeleteCategory))
	r.Post("/channels", httpx.Handle(a.CreateChannel))
	r.Delete("/channels/{id}", httpx.Handle(a.DeleteChannel))
	r.Patch("/channels/{id}", httpx.Handle(a.UpdateChannel))
	r.Put("/channels/{id}/auto-delete", httpx.Handle(a.SetAutoDelete))
	r.Post("/channels/{id}/duplicate", httpx.Handle(a.DuplicateChannel))
	r.Patch("/categories/{id}", httpx.Handle(a.RenameCategory))
	r.Put("/layout", httpx.Handle(a.ApplyLayout))
}
