package authn

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
)

// accountGeneration loads the id and token_version of an enabled account.
func (h *Handler) accountGeneration(ctx context.Context, account string) (uuid.UUID, int, error) {
	if h.generation != nil {
		return h.generation(ctx, account)
	}
	var id uuid.UUID
	var gen int
	err := h.Sessions.DB.QueryRow(ctx, `SELECT id, token_version FROM users
		WHERE LOWER(username) = $1 AND `+auth.ActiveSQL("")+` AND NOT is_bot`, account).Scan(&id, &gen)
	return id, gen, err
}

// isKnownDevice reports whether r carries a known-device cookie that is still
// valid for account: signed, unexpired, issued to this user and not retired by
// a later password change, reset, revocation or disabling.
func (h *Handler) isKnownDevice(r *http.Request, account string) bool {
	uid, gen, ok := h.Sessions.DeviceClaims(r, account)
	if !ok {
		return false
	}
	id, current, err := h.accountGeneration(r.Context(), account)
	return err == nil && id == uid && current == gen
}
