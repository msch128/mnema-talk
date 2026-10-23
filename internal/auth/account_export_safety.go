package auth

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
)

// exportBlocks lists the members the person blocked, by id and time only: the
// export names no other member.
func exportBlocks(ctx context.Context, q db.Querier, user uuid.UUID) ([]any, error) {
	type block struct {
		UserID    uuid.UUID `json:"user_id" db:"blocked_id"`
		BlockedAt time.Time `json:"blocked_at" db:"created_at"`
	}
	bs, err := collect(ctx, q, pgx.RowToStructByName[block], `SELECT blocked_id, created_at FROM user_blocks
		WHERE blocker_id = $1 ORDER BY created_at LIMIT $2`, user, exportMaxRows)
	if err != nil {
		return nil, err
	}
	out := make([]any, len(bs))
	for i, b := range bs {
		out[i] = b
	}
	return out, nil
}

// exportReports lists the reports the person filed: what they said, not the
// copy of the reported message and not what the moderators answered.
func exportReports(ctx context.Context, q db.Querier, user uuid.UUID) ([]any, error) {
	type report struct {
		MessageID uuid.UUID `json:"message_id" db:"message_id"`
		Kind      string    `json:"kind" db:"kind"`
		Category  string    `json:"category" db:"category"`
		Note      string    `json:"note" db:"note"`
		Status    string    `json:"status" db:"status"`
		CreatedAt time.Time `json:"created_at" db:"created_at"`
	}
	rs, err := collect(ctx, q, pgx.RowToStructByName[report], `SELECT message_id, kind, category, note, status, created_at
		FROM message_reports WHERE reporter_id = $1 ORDER BY created_at LIMIT $2`, user, exportMaxRows)
	if err != nil {
		return nil, err
	}
	out := make([]any, len(rs))
	for i, r := range rs {
		out[i] = r
	}
	return out, nil
}
