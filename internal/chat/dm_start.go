package chat

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// AllowDMStart applies the per-member rate limit the POST /api/dms route has
// as middleware, for callers that are not that route (the /msg command).
func (h *Handler) AllowDMStart(r *http.Request) error {
	if h.DMLimit == nil {
		return nil
	}
	if ok, _ := h.DMLimit.Allow(auth.UserKey(r)); !ok {
		return httpx.ErrRateLimited()
	}
	return nil
}

// StartDM opens the conversation between the caller and the others, or finds
// the existing one with a single other member, and announces a new one. The
// REST route and the /msg slash command both run it, so the permission, the
// community switch, the blocks and dm_policy of the others, the limits and the
// one refusal text are the same.
func (h *Handler) StartDM(r *http.Request, others []uuid.UUID) (dm DirectMessage, created bool, err error) {
	if err := h.canStartDMs(r); err != nil {
		return dm, false, err
	}
	me := auth.UserFrom(r.Context())
	id, created, err := CreateDM(r.Context(), h.DB, NewDM{Creator: me.ID, Others: others})
	if err != nil {
		return dm, false, err
	}
	dm, err = h.loadDM(r, id)
	if err != nil {
		return dm, false, err
	}
	if created {
		h.Perms.Invalidate()
		h.announceDM(dm)
	}
	return dm, created, nil
}
