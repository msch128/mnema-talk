package auth

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
)

// exportPollVotes lists the person's own poll votes. For anonymous polls it
// lists only that the person took part: what they chose was never stored.
func exportPollVotes(ctx context.Context, q db.Querier, user uuid.UUID) ([]any, error) {
	type vote struct {
		MessageID uuid.UUID `json:"message_id" db:"message_id"`
		Option    string    `json:"option" db:"label"`
		CreatedAt time.Time `json:"created_at" db:"created_at"`
	}
	vs, err := collect(ctx, q, pgx.RowToStructByName[vote], `SELECT o.poll_id AS message_id, o.label, v.created_at
		FROM poll_votes v JOIN poll_options o ON o.id = v.option_id
		JOIN polls p ON p.message_id = o.poll_id AND NOT p.anonymous
		WHERE v.user_id = $1 ORDER BY v.created_at LIMIT $2`, user, exportMaxRows)
	if err != nil {
		return nil, err
	}
	out := make([]any, len(vs))
	for i, v := range vs {
		out[i] = v
	}
	type participation struct {
		MessageID uuid.UUID `json:"message_id" db:"message_id"`
		Anonymous bool      `json:"anonymous" db:"anonymous"`
	}
	ps, err := collect(ctx, q, pgx.RowToStructByName[participation], `SELECT poll_id AS message_id, TRUE AS anonymous
		FROM poll_voters WHERE user_id = $1 LIMIT $2`, user, exportMaxRows)
	if err != nil {
		return nil, err
	}
	for _, p := range ps {
		out = append(out, p)
	}
	return out, nil
}
