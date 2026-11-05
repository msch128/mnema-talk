package search

import (
	"github.com/go-chi/chi/v5"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// API serves message search and the mentions inbox. The chat Handler builds it
// from its own fields, so the server wiring is unchanged.
type API struct {
	DB *db.Pool
	// Guard limits both to the channels the caller may view.
	Guard core.Guard
}

// Mount registers the search routes (RequireUser must already apply).
func (a *API) Mount(r chi.Router) {
	r.Get("/search", httpx.Handle(a.ServeSearch))
	r.Get("/mentions", httpx.Handle(a.ServeMentions))
}
