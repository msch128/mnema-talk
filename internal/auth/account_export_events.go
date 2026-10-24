package auth

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
)

// exportRSVPs lists the member's RSVPs to community events.
func exportRSVPs(ctx context.Context, q db.Querier, user uuid.UUID) ([]any, error) {
	type rsvpRow struct {
		EventID   uuid.UUID `json:"event_id" db:"event_id"`
		Response  string    `json:"response" db:"response"`
		CreatedAt time.Time `json:"created_at" db:"created_at"`
		UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
	}
	rs, err := collect(ctx, q, pgx.RowToStructByName[rsvpRow], `SELECT event_id, response, created_at, updated_at
		FROM event_rsvps WHERE user_id = $1 ORDER BY created_at LIMIT $2`, user, exportMaxRows)
	if err != nil {
		return nil, err
	}
	out := make([]any, len(rs))
	for i, r := range rs {
		out[i] = r
	}
	return out, nil
}
