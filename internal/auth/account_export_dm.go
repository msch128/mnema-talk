package auth

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
)

// exportDirectMessages lists the conversations the person is in: which, when
// they started and joined, and who is in them by id only (the export names no
// other member). Their own messages in them are part of messages.json; what
// others wrote is never exported.
func exportDirectMessages(ctx context.Context, q db.Querier, user uuid.UUID) ([]any, error) {
	type conversation struct {
		ID        uuid.UUID   `json:"id" db:"id"`
		Group     bool        `json:"group" db:"is_group"`
		StartedAt time.Time   `json:"started_at" db:"created_at"`
		JoinedAt  time.Time   `json:"joined_at" db:"joined_at"`
		MemberIDs []uuid.UUID `json:"member_ids" db:"member_ids"`
	}
	cs, err := collect(ctx, q, pgx.RowToStructByName[conversation], `
		SELECT c.id, c.dm_key IS NULL AS is_group, c.created_at, me.joined_at,
		       (SELECT array_agg(m.user_id ORDER BY m.joined_at, m.user_id) FROM dm_members m WHERE m.channel_id = c.id) AS member_ids
		FROM dm_members me JOIN channels c ON c.id = me.channel_id
		WHERE me.user_id = $1 ORDER BY c.created_at LIMIT $2`, user, exportMaxRows)
	if err != nil {
		return nil, err
	}
	out := make([]any, len(cs))
	for i, c := range cs {
		out[i] = c
	}
	return out, nil
}
