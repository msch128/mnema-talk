package auth

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// lockedNativeSession is a native session's account and family, both locked
// FOR UPDATE in the fixed user-first order every native transition uses.
type lockedNativeSession struct {
	user     *User
	version  int
	disabled bool
	family   nativeFamily
}

// lockNativeSession locks the user, then the family. A missing row means the
// session is gone, which callers must not tell apart from a revoked one.
func lockNativeSession(ctx context.Context, tx pgx.Tx, userID, familyID uuid.UUID) (lockedNativeSession, error) {
	user, version, disabled, err := lockNativeUser(ctx, tx, userID, false)
	if err != nil {
		return lockedNativeSession{}, nativeLockFailure(err)
	}
	family, err := lockNativeFamily(ctx, tx, familyID, user.ID, false)
	if err != nil {
		return lockedNativeSession{}, nativeLockFailure(err)
	}
	return lockedNativeSession{user: user, version: version, disabled: disabled, family: family}, nil
}

func nativeLockFailure(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNativeUnauthorized
	}
	return nativeStoreFailure(err)
}

// checkLease re-reads the database clock and verifies that the session still
// matches what the caller authenticated: same account version, family and
// instance, not revoked, and neither the family nor the access token expired.
// It returns the clock reading it used.
func (l lockedNativeSession) checkLease(ctx context.Context, tx pgx.Tx, p NativePrincipal) (time.Time, error) {
	now, err := nativeClock(ctx, tx)
	if err != nil {
		return time.Time{}, nativeStoreFailure(err)
	}
	if l.disabled || l.version != p.tokenVersion || l.family.version != l.version || l.family.instanceID != p.instanceID ||
		l.family.revokedAt != nil || !now.Before(l.family.expiresAt) || !now.Before(p.familyExpiresAt) || !now.Before(p.accessExpiresAt) {
		return time.Time{}, ErrNativeUnauthorized
	}
	return now, nil
}
