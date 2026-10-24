package perm

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// Temporary Talks (doc/talks.md) never have overwrites of their own: they mirror
// the lobby they were opened from.

// refuseTempTalk turns down a direct edit of a temporary Talk's overwrites: they
// always and only mirror the lobby it was opened from (syncTempTalks), so the
// lobby is the one place to change them.
func refuseTempTalk(ctx context.Context, tx pgx.Tx, kind string, id uuid.UUID) error {
	if kind != TargetChannel {
		return nil
	}
	// FOR UPDATE on the channel row serializes this edit with chat.OpenTempTalk,
	// which holds the same row lock on the lobby while it copies the overwrites:
	// a Talk opened concurrently with a lobby change either copies the new rules
	// or is found by syncTempTalks, never neither. Lock order: OpenTempTalk takes
	// the layout advisory lock, then this row; this path takes only the row (never
	// the advisory lock), so the two cannot wait on each other in a cycle.
	var temp bool
	if err := tx.QueryRow(ctx, `SELECT is_temp FROM channels WHERE id = $1 FOR UPDATE`, id).Scan(&temp); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("check temporary Talk: %w", err)
	}
	if temp {
		return httpx.ErrConflict("Temporary Talks follow their lobby's permissions")
	}
	return nil
}

// syncTempTalks gives the temporary Talks opened from a lobby the lobby's
// overwrites again, in the transaction that changed them: tightening a lobby
// must tighten the Talks that are already open (doc/talks.md). Replacing the
// whole list is right by design: nobody can edit a temporary Talk's overwrites
// directly (refuseTempTalk), so they hold nothing but the lobby's copy. The
// lobby edit was authorized on the lobby; the Talks derive from it. The caller's
// Changed() reloads the snapshot and sweeps voice members who lost access. It
// does nothing for a channel that is no lobby.
func syncTempTalks(ctx context.Context, tx pgx.Tx, lobby uuid.UUID) error {
	if _, err := tx.Exec(ctx, `
		DELETE FROM permission_overwrites WHERE channel_id IN
			(SELECT id FROM channels WHERE lobby_id = $1 AND is_temp)`, lobby); err != nil {
		return fmt.Errorf("clear temporary Talk overwrites: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO permission_overwrites (channel_id, role_id, user_id, allow, deny)
		SELECT t.id, o.role_id, o.user_id, o.allow, o.deny
		FROM channels t JOIN permission_overwrites o ON o.channel_id = $1
		WHERE t.lobby_id = $1 AND t.is_temp`, lobby); err != nil {
		return fmt.Errorf("copy lobby overwrites: %w", err)
	}
	return nil
}
