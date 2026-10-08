package auth

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// NativeCredentialControl performs an account-wide committed credential cutoff,
// including pending native admission. The actual Hub seals all packet gates and
// transports without waiting for voice cleanup. It never receives credentials.
type NativeCredentialControl interface {
	DisconnectNativeCredentials(uuid.UUID)
}

// changePasswordAndReissue is an irreversible native credential transition.
// It returns a NEW initial family for the same account/instance, not a refresh
// successor. Password policy and bcrypt are the browser account policy; the
// transaction additionally compares the captured hash/version under user-first
// locks. No browser cookie is issued. The caller must retire its native Core
// before dispatch; this grant cannot restore cryptographic group/device trust.
func (s *NativeSessions) changePasswordAndReissue(ctx context.Context, expected NativePrincipal, current, next string, control NativeCredentialControl) (IssuedNative, error) {
	if expected.user.ID == uuid.Nil || expected.familyID == uuid.Nil || expected.instanceID == uuid.Nil || control == nil {
		return IssuedNative{}, ErrNativeUnauthorized
	}
	if err := ValidatePassword(next); err != nil {
		return IssuedNative{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, nativeQueryTimeout)
	defer cancel()
	actual, _, err := s.authenticateNativeHash(ctx, expected.accessHash)
	if err != nil {
		return IssuedNative{}, err
	}
	if !expected.SameNativeAccess(actual) {
		return IssuedNative{}, ErrNativeUnauthorized
	}
	var capturedHash string
	err = s.pool.QueryRow(ctx, `SELECT password_hash FROM users WHERE id=$1 AND token_version=$2 AND disabled_at IS NULL`, expected.user.ID, expected.tokenVersion).Scan(&capturedHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return IssuedNative{}, ErrNativeUnauthorized
	}
	if err != nil {
		return IssuedNative{}, nativeStoreFailure(err)
	}
	if !CheckPassword(current, capturedHash) {
		return IssuedNative{}, ErrWrongPassword
	}
	newHash, err := HashPassword(next)
	if err != nil {
		return IssuedNative{}, nativeStoreFailure(err)
	}
	access, refresh, err := generateNativeSecrets(s.random)
	if err != nil {
		return IssuedNative{}, err
	}
	familyID, err := uuid.NewRandomFromReader(s.random)
	if err != nil {
		return IssuedNative{}, errNativeEntropy
	}
	var result IssuedNative
	wrote := false
	defer func() {
		// A failed COMMIT acknowledgement can mean that PostgreSQL committed.
		// Return no grant, and seal old transports conservatively after any
		// successful credential write. A pre-write stale loser must not close
		// a concurrent winner's new family. Never retry this transition.
		if wrote {
			control.DisconnectNativeCredentials(expected.user.ID)
		}
	}()
	err = s.transact(ctx, func(ctx context.Context, tx pgx.Tx) error {
		user, version, disabled, err := lockNativeUser(ctx, tx, expected.user.ID, false)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNativeUnauthorized
		}
		if err != nil {
			return nativeStoreFailure(err)
		}
		family, err := lockNativeFamily(ctx, tx, expected.familyID, user.ID, false)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNativeUnauthorized
		}
		if err != nil {
			return nativeStoreFailure(err)
		}
		valid := func() (time.Time, error) {
			now, err := nativeClock(ctx, tx)
			if err != nil {
				return time.Time{}, nativeStoreFailure(err)
			}
			if disabled || version != expected.tokenVersion || family.version != version || family.instanceID != expected.instanceID || family.revokedAt != nil || !family.expiresAt.Equal(expected.familyExpiresAt) || !now.Before(family.expiresAt) || !now.Before(expected.accessExpiresAt) {
				return time.Time{}, ErrNativeUnauthorized
			}
			return now, nil
		}
		now, err := valid()
		if err != nil {
			return err
		}
		var lockedHash string
		if err = tx.QueryRow(ctx, `SELECT password_hash FROM users WHERE id=$1`, user.ID).Scan(&lockedHash); err != nil {
			return nativeStoreFailure(err)
		}
		if lockedHash != capturedHash {
			return ErrNativeUnauthorized
		}
		var accessExpiry time.Time
		if err = tx.QueryRow(ctx, `SELECT expires_at FROM native_access_tokens WHERE token_hash=$1 AND family_id=$2`, expected.accessHash[:], expected.familyID).Scan(&accessExpiry); errors.Is(err, pgx.ErrNoRows) {
			return ErrNativeUnauthorized
		} else if err != nil {
			return nativeStoreFailure(err)
		}
		if !accessExpiry.Equal(expected.accessExpiresAt) {
			return ErrNativeUnauthorized
		}
		var nextVersion int
		if err = tx.QueryRow(ctx, `UPDATE users SET password_hash=$1,token_version=token_version+1,updated_at=NOW() WHERE id=$2 RETURNING token_version`, newHash, user.ID).Scan(&nextVersion); err != nil {
			return nativeStoreFailure(err)
		}
		wrote = true
		// Existing version_mismatch is the accurate reason for these old
		// families; no applied migration or revocation enum is changed.
		if _, err = tx.Exec(ctx, `UPDATE native_session_families SET revoked_at=$2,revoke_reason='version_mismatch' WHERE user_id=$1 AND revoked_at IS NULL`, user.ID, now); err != nil {
			return nativeStoreFailure(err)
		}
		expiry := now.Add(s.policy.FamilyLifetime)
		if _, err = tx.Exec(ctx, `INSERT INTO native_session_families(id,user_id,client_instance_id,issued_token_version,created_at,expires_at,refresh_after) VALUES($1,$2,$3,$4,$5,$6,$7)`, familyID, user.ID, expected.instanceID, nextVersion, now, expiry, now.Add(nativeRefreshInterval)); err != nil {
			return nativeStoreFailure(err)
		}
		accessExpiry, err = insertNativeTokens(ctx, tx, familyID, 0, now, expiry, access, refresh)
		if err != nil {
			return nativeStoreFailure(err)
		}
		if _, err = valid(); err != nil {
			return err
		}
		result = IssuedNative{principal: NativePrincipal{user: *user, familyID: familyID, instanceID: expected.instanceID, tokenVersion: nextVersion, accessExpiresAt: accessExpiry, familyExpiresAt: expiry, accessHash: access.digest()}, access: access, refresh: refresh}
		return nil
	})
	if err != nil {
		return IssuedNative{}, err
	}
	return result, nil
}
