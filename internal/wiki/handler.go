package wiki

import (
	"context"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/media"
	"github.com/msch128/mnema-talk/internal/perm"
)

// Handler serves the wiki API (doc/wiki.md).
type Handler struct {
	Store *Store
	Perms *perm.Service
	Audit *audit.Log
	// MediaStore and MediaBucket keep the pictures; nil MediaStore turns the upload off.
	MediaStore  media.Store
	MediaBucket string
	// Reserve checks an upload against the storage budgets and holds the room until released; nil imposes none.
	Reserve func(ctx context.Context, user uuid.UUID, size int64) (release func(), err error)
	// WriteLimit caps the writes of one member; nil means no cap.
	WriteLimit *httpx.RateLimiter
}

// Mount registers the routes (RequireUser must already apply).
func (h *Handler) Mount(r chi.Router) {
	r.Get("/wiki", httpx.Handle(h.list))
	r.Get("/wiki/search", httpx.Handle(h.search))
	r.Get("/wiki/pages/{slug}", httpx.Handle(h.get))
	r.Get("/wiki/pages/{slug}/revisions", httpx.Handle(h.revisions))
	r.Get("/wiki/pages/{slug}/revisions/{rev}", httpx.Handle(h.revision))
	writes := r
	if h.WriteLimit != nil {
		writes = r.With(h.WriteLimit.By(auth.UserKey))
	}
	writes.Post("/wiki/pages", httpx.Handle(h.create))
	writes.Patch("/wiki/pages/{slug}", httpx.Handle(h.update))
	writes.Delete("/wiki/pages/{slug}", httpx.Handle(h.remove))
	writes.Post("/wiki/pages/{slug}/undelete", httpx.Handle(h.undelete))
	writes.Put("/wiki/pages/{slug}/lock", httpx.Handle(h.lock))
	writes.Post("/wiki/pages/{slug}/revisions/{rev}/restore", httpx.Handle(h.restore))
}

// require checks a server-wide permission of the caller (a timeout, pending
// rules and the new-member wait count for the ones they take away).
func (h *Handler) require(r *http.Request, need perm.Set) error {
	return h.Perms.RequireGlobal(r.Context(), auth.UserFrom(r.Context()).Subject(), need)
}

// requireWrite checks what every change of the wiki needs: to read it, to edit
// it, and what the change itself asks for on top (Manage wiki for deleting,
// locking, renaming and moving). Managing alone changes nothing, and a caller
// who may not read the wiki gets no page back from a write either.
func (h *Handler) requireWrite(r *http.Request, extra perm.Set) error {
	return h.require(r, perm.ViewWiki|perm.EditWiki|extra)
}

// can reports whether the caller holds the permission (false when it cannot be told).
func (h *Handler) can(r *http.Request, need perm.Set) bool {
	return h.require(r, need) == nil
}

func (h *Handler) record(r *http.Request, action string, p *Page, details map[string]any) {
	if h.Audit == nil || p == nil {
		return
	}
	h.Audit.RecordOrLog(r.Context(), audit.Entry{
		Action: action, TargetType: "wiki_page", TargetID: p.ID.String(), TargetName: p.Title, Details: details,
	})
}

func pathRev(r *http.Request) (int, error) {
	rev, err := strconv.Atoi(chi.URLParam(r, "rev"))
	if err != nil || rev < 1 {
		return 0, httpx.ErrInvalidInput("rev must be a revision number")
	}
	return rev, nil
}

func (h *Handler) userID(r *http.Request) uuid.UUID { return auth.UserFrom(r.Context()).ID }
