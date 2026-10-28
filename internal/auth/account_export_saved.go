package auth

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/db"
)

// exportSaved lists the person's saved messages by id, their own note and the
// time. The saved text belongs to other people as much as to the saver and is
// not exported; the person's own messages are in messages.json.
func exportSaved(ctx context.Context, q db.Querier, user uuid.UUID) (capped, error) {
	type saved struct {
		MessageID uuid.UUID `json:"message_id" db:"message_id"`
		Note      string    `json:"note" db:"note"`
		SavedAt   time.Time `json:"saved_at" db:"created_at"`
	}
	return listDataset[saved](ctx, q, user, `SELECT message_id, note, created_at
		FROM saved_messages WHERE user_id = $1 ORDER BY created_at LIMIT $2`)
}
