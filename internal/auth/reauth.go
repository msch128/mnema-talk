package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// CheckUserPassword verifies password against userID's stored hash. A wrong
// password returns ErrWrongPassword; a missing or disabled user does too
// (after the same bcrypt work), so callers can't tell the cases apart.
func CheckUserPassword(ctx context.Context, p *db.Pool, userID uuid.UUID, password string) error {
	var hash string
	err := p.QueryRow(ctx, `SELECT password_hash FROM users WHERE id = $1 AND disabled_at IS NULL`, userID).Scan(&hash)
	if errors.Is(err, pgx.ErrNoRows) {
		burnPasswordCheck(password)
		return ErrWrongPassword
	}
	if err != nil {
		return fmt.Errorf("load password hash: %w", err)
	}
	if !CheckPassword(password, hash) {
		return ErrWrongPassword
	}
	return nil
}

// ConfirmPassword re-authenticates the signed-in user before a sensitive
// action (e.g. the self-update). It shares the lockout of PUT
// /api/auth/password (5 wrong passwords, then escalating pauses per user),
// so alternating between the two endpoints gains an attacker nothing.
// While locked out it returns httpx.ErrRateLimited() and the wait.
func (h *Handler) ConfirmPassword(ctx context.Context, userID uuid.UUID, password string) (time.Duration, error) {
	key := userID.String()
	if locked, retry := h.passwordFails.IsLockedOut(key); locked {
		return retry, httpx.ErrRateLimited()
	}
	if password == "" {
		h.passwordFails.RecordFailure(key)
		return 0, ErrWrongPassword
	}
	err := CheckUserPassword(ctx, h.Sessions.DB, userID, password)
	if errors.Is(err, ErrWrongPassword) {
		h.passwordFails.RecordFailure(key)
		return 0, err
	}
	if err != nil {
		return 0, err
	}
	h.passwordFails.ResetFailures(key)
	return 0, nil
}
