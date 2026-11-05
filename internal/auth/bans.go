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
