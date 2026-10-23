package auth

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
)

// exportMaxEdits bounds the earlier versions in an export: each can be 4000
// characters, and they are held in memory while the list is built.
const exportMaxEdits = 2000

// exportEdits lists the earlier versions of the person's own edited messages
// (what moderators can read as the edit history), newest first. Only their
// own messages and their own text: other members' edits never appear here.
func exportEdits(ctx context.Context, q db.Querier, user uuid.UUID) (map[string]any, error) {
	type version struct {
		MessageID  uuid.UUID `json:"message_id" db:"message_id"`
		Content    string    `json:"content" db:"content"`
		EditedAt   time.Time `json:"edited_at" db:"edited_at"`
		ReplacedAt time.Time `json:"replaced_at" db:"replaced_at"`
	}
	vs, err := collect(ctx, q, pgx.RowToStructByName[version], `SELECT e.message_id, e.content, e.edited_at, e.replaced_at
		FROM message_edits e JOIN messages m ON m.id = e.message_id
		WHERE e.editor_id = $1 AND m.user_id = $1 ORDER BY e.id DESC LIMIT $2`, user, exportMaxEdits+1)
	if err != nil {
		return nil, err
	}
	truncated := len(vs) > exportMaxEdits
	if truncated {
		vs = vs[:exportMaxEdits]
	}
	return map[string]any{"edits": vs, "truncated": truncated}, nil
}
