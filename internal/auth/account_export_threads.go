package auth

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/db"
)

// exportThreadFollows lists the threads the person follows or has explicitly
// unfollowed: the thread's root message id and the state, never the thread's
// text or who else took part.
func exportThreadFollows(ctx context.Context, q db.Querier, user uuid.UUID) (capped, error) {
	type follow struct {
		ThreadID  uuid.UUID `json:"thread_id" db:"thread_id"`
		Following bool      `json:"following" db:"following"`
		Since     time.Time `json:"since" db:"created_at"`
	}
	return listDataset[follow](ctx, q, user, `SELECT thread_id, following, created_at
		FROM thread_follows WHERE user_id = $1 ORDER BY created_at, thread_id LIMIT $2`)
}
