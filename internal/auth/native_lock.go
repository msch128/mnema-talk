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
	return l.clockLease(ctx, tx, func(now time.Time) bool { return l.leaseRejected(now, p) })
}

// checkReissueLease verifies, at the database clock, that the session still is
// the one the caller authenticated: same account version, family, instance and
// deadlines, and not revoked. It returns the clock reading it used.
func (l lockedNativeSession) checkReissueLease(ctx context.Context, tx pgx.Tx, expected NativePrincipal) (time.Time, error) {
	return l.clockLease(ctx, tx, func(now time.Time) bool { return l.reissueLeaseRejected(now, expected) })
}

func (l lockedNativeSession) clockLease(ctx context.Context, tx pgx.Tx, rejected func(time.Time) bool) (time.Time, error) {
	now, err := nativeClock(ctx, tx)
	if err != nil {
		return time.Time{}, nativeStoreFailure(err)
	}
	if rejected(now) {
		return time.Time{}, ErrNativeUnauthorized
	}
	return now, nil
}

// leaseRejected reports that the locked session is not the one p authenticated.
func (l lockedNativeSession) leaseRejected(now time.Time, p NativePrincipal) bool {
	family := l.family
	if l.disabled || l.version != p.tokenVersion {
		return true
	}
	if family.version != l.version || family.instanceID != p.instanceID {
		return true
	}
	if family.revokedAt != nil || !now.Before(family.expiresAt) {
		return true
	}
	return !now.Before(p.familyExpiresAt) || !now.Before(p.accessExpiresAt)
}

// reissueLeaseRejected reports that the locked session is not the one the
// caller authenticated, read at the database clock now. A reissue also requires
// the stored family deadline to be the one the caller was shown.
func (l lockedNativeSession) reissueLeaseRejected(now time.Time, expected NativePrincipal) bool {
	family := l.family
	if l.disabled || l.version != expected.tokenVersion {
		return true
	}
	if family.version != l.version || family.instanceID != expected.instanceID {
		return true
	}
	if family.revokedAt != nil || !family.expiresAt.Equal(expected.familyExpiresAt) {
		return true
	}
	return !now.Before(family.expiresAt) || !now.Before(expected.accessExpiresAt)
}
