package auth

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func isInvalidAuthorizedPrincipal(principal NativePrincipal, action func(context.Context, pgx.Tx, User) error) bool {
	if principal.user.ID == uuid.Nil || principal.familyID == uuid.Nil {
		return true
	}
	return principal.instanceID == uuid.Nil || action == nil
}

// WithAuthorizedTransaction serializes a native mutation with account and family
// revocation. The principal must originate from AuthenticateAccess. This grants
// transport authorization only, never device, group or ciphertext authenticity.
// Captured access validity is bounded by its original authenticated deadline;
// account/family revocation is re-read under locks. The current policy has no
// individual access-token revocation operation; token garbage collection does
// not extend that deadline. Actions receive the current account, and must not
// leak transaction handles.
func (s *NativeSessions) WithAuthorizedTransaction(ctx context.Context, principal NativePrincipal, action func(context.Context, pgx.Tx, User) error) error {
	if isInvalidAuthorizedPrincipal(principal, action) {
		return ErrNativeUnauthorized
	}
	ctx, cancel := context.WithTimeout(ctx, nativeQueryTimeout)
	defer cancel()
	return s.transact(ctx, func(ctx context.Context, tx pgx.Tx) error {
		locked, err := lockNativeSession(ctx, tx, principal.user.ID, principal.familyID)
		if err != nil {
			return err
		}
		if _, err := locked.checkLease(ctx, tx, principal); err != nil {
			return err
		}
		if err := action(ctx, tx, *locked.user); err != nil {
			return err
		}
		// A blocked action must not commit using a lease that expired during its SQL.
		_, err = locked.checkLease(ctx, tx, principal)
		return err
	})
}
