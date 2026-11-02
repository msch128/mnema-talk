package auth

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/db"
)

// exportRSVPs lists the member's RSVPs to community events.
func exportRSVPs(ctx context.Context, q db.Querier, user uuid.UUID) (capped, error) {
	type rsvpRow struct {
		EventID   uuid.UUID `json:"event_id" db:"event_id"`
		Response  string    `json:"response" db:"response"`
		CreatedAt time.Time `json:"created_at" db:"created_at"`
		UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
	}
	return listDataset[rsvpRow](ctx, q, user, `SELECT event_id, response, created_at, updated_at
		FROM event_rsvps WHERE user_id = $1 ORDER BY created_at LIMIT $2`)
}

// exportEventSignups lists the member's sign-ups for event roles.
func exportEventSignups(ctx context.Context, q db.Querier, user uuid.UUID) (capped, error) {
	type signupRow struct {
		EventID   uuid.UUID `json:"event_id" db:"event_id"`
		Role      string    `json:"role" db:"role"`
		State     string    `json:"state" db:"state"`
		CreatedAt time.Time `json:"created_at" db:"created_at"`
	}
	return listDataset[signupRow](ctx, q, user, `SELECT s.event_id, r.name AS role, s.state, s.created_at
		FROM event_signups s JOIN event_roles r ON r.id = s.role_id
		WHERE s.user_id = $1 ORDER BY s.created_at LIMIT $2`)
}

// exportEventComments lists the member's comments on community events.
func exportEventComments(ctx context.Context, q db.Querier, user uuid.UUID) (capped, error) {
	type commentRow struct {
		EventID   uuid.UUID  `json:"event_id" db:"event_id"`
		Content   string     `json:"content" db:"content"`
		CreatedAt time.Time  `json:"created_at" db:"created_at"`
		EditedAt  *time.Time `json:"edited_at,omitempty" db:"edited_at"`
	}
	return listDataset[commentRow](ctx, q, user, `SELECT event_id, content, created_at, edited_at
		FROM event_comments WHERE user_id = $1 ORDER BY created_at LIMIT $2`)
}
