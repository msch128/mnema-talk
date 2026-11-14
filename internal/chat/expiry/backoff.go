package expiry

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	// heldFirst is the pause after a message first failed to delete; it
	// doubles with every failure up to heldMax.
	heldFirst = time.Second
	heldMax   = 10 * time.Minute
)

// heldBack are the ended messages whose delete failed: each is tried again
// after a pause that grows, so one bad row cannot stop the others (they are
// left out of the batches meanwhile) nor spin the loop.
type heldBack struct {
	mu   sync.Mutex
	rows map[uuid.UUID]held
}

type held struct {
	until time.Time
	tries int
}

// fail books another pause for id.
func (h *heldBack) fail(id uuid.UUID, now time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.rows == nil {
		h.rows = map[uuid.UUID]held{}
	}
	r := h.rows[id]
	r.tries++
	pause := heldFirst << min(r.tries-1, 20)
	r.until = now.Add(min(pause, heldMax))
	h.rows[id] = r
}

// done forgets id: it was deleted, or is gone.
func (h *heldBack) done(id uuid.UUID) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.rows, id)
}

// waiting lists the ids whose pause has not run out, and the earliest moment
// one of them is due again (zero when none waits).
func (h *heldBack) waiting(now time.Time) ([]uuid.UUID, time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	ids := []uuid.UUID{} // never nil: "id = ANY(NULL)" would leave out every row
	var next time.Time
	for id, r := range h.rows {
		if !r.until.After(now) {
			continue
		}
		ids = append(ids, id)
		if next.IsZero() || r.until.Before(next) {
			next = r.until
		}
	}
	return ids, next
}
