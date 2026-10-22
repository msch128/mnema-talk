package bot

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// minterRecheck is how long a verdict about a token's minter is reused. A
// demotion therefore ends the token's use within this time, without walking
// the bot's permissions on every request.
const minterRecheck = 30 * time.Second

type minterVerdict struct {
	ok    bool
	until time.Time
}

// CheckMinter refuses a token whose issuer no longer qualifies: the account
// must be active and either the Owner, or hold Manage bots and still be able
// to act as the bot (it holds everything the bot can do, see CanActAs). A
// token without a recorded minter (issued before the column existed) is not
// checked. Failing to decide fails the request.
func (a *Authenticator) CheckMinter(ctx context.Context, tokenID uuid.UUID, minter *uuid.UUID, botUser uuid.UUID) error {
	if minter == nil || a.Perms == nil {
		return nil
	}
	now := a.now()
	a.mu.Lock()
	v, seen := a.minters[tokenID]
	a.mu.Unlock()
	if !seen || now.After(v.until) {
		ok, err := a.minterQualifies(ctx, *minter, botUser)
		if err != nil {
			return err
		}
		v = minterVerdict{ok: ok, until: now.Add(minterRecheck)}
		a.mu.Lock()
		if a.minters == nil || len(a.minters) > 4096 {
			a.minters = map[uuid.UUID]minterVerdict{}
		}
		a.minters[tokenID] = v
		a.mu.Unlock()
	}
	if !v.ok {
		return errUnauthorized
	}
	return nil
}

func (a *Authenticator) minterQualifies(ctx context.Context, minter, botUser uuid.UUID) (bool, error) {
	u, err := auth.GetUser(ctx, a.DB, minter)
	if apiErr, isAPI := httpx.AsAPIError(err); isAPI && apiErr.Status == http.StatusNotFound {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var active bool
	if err := a.DB.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE id = $1 AND `+auth.ActiveSQL("")+`)`, minter).Scan(&active); err != nil || !active {
		return false, err
	}
	snap, err := a.Perms.Snapshot(ctx)
	if err != nil {
		return false, err
	}
	sub := u.Subject()
	if snap.IsOwner(sub) {
		return true, nil
	}
	if !snap.Global(sub).Has(perm.ManageBots) {
		return false, nil
	}
	return snap.CanActAs(sub, perm.Subject{ID: botUser}) == nil, nil
}
