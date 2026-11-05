package auth

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// DeletedUsernamePrefix starts the placeholder name of a deleted account. No
// new account may take a name with it (see ValidateUsername).
const DeletedUsernamePrefix = "deleted-"

// refuseDeleted makes a deleted account look like one that never existed to
// every administrative action: it cannot be enabled, given a password, banned,
// timed out or given roles again. The database enforces the same with a
// trigger, so no code path can revive a tombstone.
func refuseDeleted(ctx context.Context, q db.Querier, id uuid.UUID) error {
	var deleted bool
	err := q.QueryRow(ctx, `SELECT deleted_at IS NOT NULL FROM users WHERE id = $1`, id).Scan(&deleted)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && deleted) {
		return httpx.ErrNotFound("user not found")
	}
	return err
}
