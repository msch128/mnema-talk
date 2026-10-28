package auth

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/db"
)

// exportSessions lists the member's own sessions: coarse device label, when
// each was created and last used, and when it expires. Never the session id.
func exportSessions(ctx context.Context, q db.Querier, user uuid.UUID) (capped, error) {
	return listDataset[exportedSession](ctx, q, user, `
		SELECT device, created_at, last_active_at, expires_at FROM user_sessions WHERE user_id = $1 ORDER BY created_at LIMIT $2`)
}

type exportedSession struct {
	Device       string    `json:"device" db:"device"`
	CreatedAt    time.Time `json:"created_at" db:"created_at"`
	LastActiveAt time.Time `json:"last_active_at" db:"last_active_at"`
	ExpiresAt    time.Time `json:"expires_at" db:"expires_at"`
}
