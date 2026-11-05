package auth

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// What makes an account unusable: inactive (disabled or banned), banned, or
// deleted.

// An account can act (sign in, keep a session, open a socket) only when it is
// neither disabled nor banned. The two states are separate columns so that
// lifting one never lifts the other: disabled_at is set by Disable, banned_at
// by Ban, and a ban whose banned_until has passed no longer counts even if the
// expiry job has not cleared it yet. Every query that decides whether an
// account may act builds its condition from here.

// ActiveSQL is the condition "this account may act"; alias qualifies the
// users table ("" for none).
func ActiveSQL(alias string) string {
	return "NOT " + InactiveSQL(alias)
}

// InactiveSQL is the condition "this account is disabled or banned".
func InactiveSQL(alias string) string {
	p := alias
	if p != "" {
		p += "."
	}
	return "(" + p + "disabled_at IS NOT NULL OR " + p + "deleted_at IS NOT NULL OR (" + p + "banned_at IS NOT NULL AND (" + p + "banned_until IS NULL OR " + p + "banned_until > NOW())))"
}

// bannedError is the sign-in refusal for a banned account: the member sees
// why and until when.
func bannedError(ctx context.Context, p db.Querier, id uuid.UUID) error {
	var reason string
	var until *time.Time
	err := p.QueryRow(ctx, `SELECT ban_reason, banned_until FROM users
		WHERE id = $1 AND banned_at IS NOT NULL AND (banned_until IS NULL OR banned_until > NOW())`, id).Scan(&reason, &until)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrAccountDisabled
	}
	if err != nil {
		return err
	}
	msg := "this account is banned"
	if until != nil {
		msg += " until " + until.UTC().Format("2006-01-02 15:04 UTC")
	}
	if reason != "" {
		msg += ": " + reason
	}
	return httpx.NewAPIError(http.StatusForbidden, httpx.CodeAccountDisabled, msg)
}

// DeletedUsernamePrefix starts the placeholder name of a deleted account. No
// new account may take a name with it (see ValidateUsername).
const DeletedUsernamePrefix = "deleted-"

// RefuseDeleted makes a deleted account look like one that never existed to
// every administrative action: it cannot be enabled, given a password, banned,
// timed out or given roles again. The database enforces the same with a
// trigger, so no code path can revive a tombstone.
func RefuseDeleted(ctx context.Context, q db.Querier, id uuid.UUID) error {
	var deleted bool
	err := q.QueryRow(ctx, `SELECT deleted_at IS NOT NULL FROM users WHERE id = $1`, id).Scan(&deleted)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && deleted) {
		return httpx.ErrNotFound("user not found")
	}
	return err
}
