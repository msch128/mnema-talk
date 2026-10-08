package auth

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// WithAuthorizedTransaction serializes a native mutation with account and family
// revocation. The principal must originate from AuthenticateAccess. This grants
// transport authorization only, never device, group or ciphertext authenticity.
// Captured access validity is bounded by its original authenticated deadline;
// account/family revocation is re-read under locks. The current policy has no
// individual access-token revocation operation; token garbage collection does
// not extend that deadline. Actions receive the current account, and must not
// leak transaction handles.
func (s *NativeSessions) WithAuthorizedTransaction(ctx context.Context, principal NativePrincipal, action func(context.Context, pgx.Tx, User) error) error {
	if principal.user.ID == uuid.Nil || principal.familyID == uuid.Nil || principal.instanceID == uuid.Nil || action == nil {
		return ErrNativeUnauthorized
	}
	ctx, cancel := context.WithTimeout(ctx, nativeQueryTimeout)
	defer cancel()
	return s.transact(ctx, func(ctx context.Context, tx pgx.Tx) error {
		user, version, disabled, err := lockNativeUser(ctx, tx, principal.user.ID, false)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNativeUnauthorized
		}
		if err != nil {
			return nativeStoreFailure(err)
		}
		family, err := lockNativeFamily(ctx, tx, principal.familyID, user.ID, false)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNativeUnauthorized
		}
		if err != nil {
			return nativeStoreFailure(err)
		}
		valid := func() error {
			now, err := nativeClock(ctx, tx)
			if err != nil {
				return nativeStoreFailure(err)
			}
			if disabled || version != principal.tokenVersion || family.version != version || family.instanceID != principal.instanceID || family.revokedAt != nil || !now.Before(family.expiresAt) || !now.Before(principal.familyExpiresAt) || !now.Before(principal.accessExpiresAt) {
				return ErrNativeUnauthorized
			}
			return nil
		}
		if err := valid(); err != nil {
			return err
		}
		if err := action(ctx, tx, *user); err != nil {
			return err
		}
		// A blocked action must not commit using a lease that expired during its SQL.
		return valid()
	})
}
