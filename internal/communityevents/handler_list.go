package communityevents

import (
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// listQuery reads after, before and limit and builds the visibility check
// from one permission snapshot. Without a snapshot nothing in a Talk is shown.
func (h *Handler) listQuery(r *http.Request) (ListQuery, error) {
	now := time.Now()
	q := ListQuery{
		After:  now.Add(-listLookBehind),
		Before: now.Add(listLookAhead),
		Limit:  httpx.QueryLimit(r, "limit", DefaultListLimit, MaxListLimit),
	}
	switch when := r.URL.Query().Get("when"); when {
	case "", "upcoming":
	case "past":
		q.Past = true
		q.Before = now.AddDate(100, 0, 0) // no cursor: start from the newest
	default:
		return q, httpx.ErrInvalidInput("when must be upcoming or past")
	}
	if raw := r.URL.Query().Get("before_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			return q, httpx.ErrInvalidInput("before_id must be a UUID")
		}
		q.BeforeID = id
	}
	for name, dst := range map[string]*time.Time{"after": &q.After, "before": &q.Before} {
		raw := r.URL.Query().Get(name)
		if raw == "" {
			continue
		}
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return q, httpx.ErrInvalidInput(name + " must be an RFC 3339 time")
		}
		*dst = t
	}
	if h.Perms == nil {
		return q, nil
	}
	snap, err := h.Perms.Snapshot(r.Context())
	if err != nil {
		return q, err
	}
	sub := auth.UserFrom(r.Context()).Subject()
	q.Allow = func(id uuid.UUID) bool { return snap.Channel(sub, id).Has(perm.ViewChannel) }
	return q, nil
}
