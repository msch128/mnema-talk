package authn

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/perm"
)

// Moderated runs fn on the member a moderation action aims at, after checking
// that the caller may act on them. The check and fn run in one transaction
// under the permission lock, on the state as it is inside it: a concurrent
// role change cannot move the target out of reach (or the caller down) between
// the check and the write.
func (h *Handler) Moderated(r *http.Request, id uuid.UUID, fn func(q db.Querier) error) error {
	ctx := r.Context()
	actor := auth.UserFrom(ctx)
	if err := auth.RefuseDeleted(ctx, h.Sessions.DB, id); err != nil {
		return err
	}
	return h.Perms.Locked(ctx, actor.Subject(), func(tx pgx.Tx, snap *perm.Snapshot) error {
		target, err := auth.GetUser(ctx, tx, id)
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
func checkTarget(snap *perm.Snapshot, actor, target *auth.User) error {
	if target.IsAdmin() {
		return auth.ErrOwnerAction
	}
	if actor == nil {
		return auth.ErrNoSession
	}
	if actor.IsAdmin() {
		return nil
	}
	return snap.CanModerate(actor.Subject(), target.Subject())
}
