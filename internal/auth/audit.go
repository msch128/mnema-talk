package auth

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/msch128/mnema-talk/internal/audit"
)

// failureAuditEvery is the shortest gap between two audit entries for failed
// or blocked sign-ins of the same account, so guessing cannot flood the log.
const failureAuditEvery = time.Minute

// auditUser records an event about a signed-in or just-registered user.
func (h *Handler) auditUser(ctx context.Context, u *User, action string) {
	if h.Audit == nil {
		return
	}
	id := u.ID
	h.Audit.RecordOrLog(ctx, audit.Entry{
		ActorID: &id, ActorName: u.Username, Action: action,
		TargetType: "user", TargetID: id.String(), TargetName: u.Username,
	})
}

// auditLoginFailure records a wrong password or a lockout for an account
// that exists. Names that match no account are not stored: what was typed
// into the username field may be anything, even a password. At most one
// entry per account and action and failureAuditEvery.
func (h *Handler) auditLoginFailure(ctx context.Context, account, action string) {
	if h.Audit == nil {
		return
	}
	key := action + "|" + account
	now := time.Now()
	if h.failureSeen != nil {
		if last, ok := h.failureSeen.Load(key); ok && now.Sub(last.(time.Time)) < failureAuditEvery {
			return
		}
	}
	var id uuid.UUID
	var name string
	if err := h.Sessions.DB.QueryRow(ctx, `SELECT id, username FROM users WHERE lower(username) = $1`, account).Scan(&id, &name); err != nil {
		return
	}
	if h.failureSeen != nil {
		h.failureSeen.Store(key, now)
	}
	h.Audit.RecordOrLog(ctx, audit.Entry{
		Action: action, TargetType: "user", TargetID: id.String(), TargetName: name,
	})
}
