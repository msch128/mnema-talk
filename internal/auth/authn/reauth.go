package authn

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/httpx"
)

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
		return 0, auth.ErrWrongPassword
	}
	err := auth.CheckUserPassword(ctx, h.Sessions.DB, userID, password)
	if errors.Is(err, auth.ErrWrongPassword) {
		h.passwordFails.RecordFailure(key)
		return 0, err
	}
	if err != nil {
		return 0, err
	}
	h.passwordFails.ResetFailures(key)
	return 0, nil
}
