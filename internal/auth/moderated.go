package auth

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// errOwnerAction is the refusal of an admin action aimed at the Owner account.
var errOwnerAction = httpx.ErrInvalidInput("this action is not available for the Owner account")

// Moderated runs fn on the member a moderation action aims at, after checking
// that the caller may act on them. With a permission service the check and fn
// run in one transaction under the permission lock, on the state as it is
// inside it: a concurrent role change cannot move the target out of reach (or
// the caller down) between the check and the write.
func (h *Handler) Moderated(r *http.Request, id uuid.UUID, fn func(q db.Querier) error) error {
	ctx := r.Context()
	actor := UserFrom(ctx)
	if err := refuseDeleted(ctx, h.Sessions.DB, id); err != nil {
		return err
	}
	if h.Perms == nil {
		target, err := GetUser(ctx, h.Sessions.DB, id)
		if err != nil {
			return err
		}
		if err := checkTarget(nil, actor, target); err != nil {
			return err
		}
		return fn(h.Sessions.DB)
	}
	return h.Perms.Locked(ctx, actor.Subject(), func(tx pgx.Tx, snap *perm.Snapshot) error {
		target, err := GetUser(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := checkTarget(snap, actor, target); err != nil {
			return err
		}
		return fn(tx)
	})
}

// checkTarget: never the Owner, only members below the caller's highest role.
func checkTarget(snap *perm.Snapshot, actor, target *User) error {
	if target.IsAdmin() {
		return errOwnerAction
	}
	if actor == nil {
		return errNoSession
	}
	if snap == nil || actor.IsAdmin() {
		return nil
	}
	return snap.CanModerate(actor.Subject(), target.Subject())
}
