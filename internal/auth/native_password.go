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

func reissueCallerMissing(expected NativePrincipal, control NativeCredentialControl) bool {
	if expected.user.ID == uuid.Nil || expected.familyID == uuid.Nil {
		return true
	}
	return expected.instanceID == uuid.Nil || control == nil
}

// changePasswordAndReissue is an irreversible native credential transition.
// It returns a NEW initial family for the same account/instance, not a refresh
// successor. Password policy and bcrypt are the browser account policy; the
// transaction additionally compares the captured hash/version under user-first
// locks. No browser cookie is issued. The caller must retire its native Core
// before dispatch; this grant cannot restore cryptographic group/device trust.
func (s *NativeSessions) changePasswordAndReissue(ctx context.Context, expected NativePrincipal, current, next string, control NativeCredentialControl) (IssuedNative, error) {
	if reissueCallerMissing(expected, control) {
		return IssuedNative{}, ErrNativeUnauthorized
	}
	if err := ValidatePasswordFor(expected.user.Role, next); err != nil {
		return IssuedNative{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, nativeQueryTimeout)
	defer cancel()
	change, err := s.prepareReissue(ctx, expected, current, next)
	if err != nil {
		return IssuedNative{}, err
	}
	defer func() {
		// A failed COMMIT acknowledgement can mean that PostgreSQL committed.
		// Return no grant, and seal old transports conservatively after any
		// successful credential write. A pre-write stale loser must not close
		// a concurrent winner's new family. Never retry this transition.
		if change.wrote {
			control.DisconnectNativeCredentials(expected.user.ID)
		}
	}()
	if err := s.transact(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return s.reissue(ctx, tx, change)
	}); err != nil {
		return IssuedNative{}, err
	}
	return change.result, nil
}

// passwordReissue is one password change: what was checked before the
// transaction and, once it ran, what it produced.
type passwordReissue struct {
	expected     NativePrincipal
	capturedHash string
	newHash      string
	familyID     uuid.UUID
	access       nativeSecret
	refresh      nativeSecret
	// wrote is set once the new credentials are written.
	wrote  bool
	result IssuedNative
}

// prepareReissue authenticates the caller's access token, checks the current
// password and prepares the hash, family ID and tokens of the new session, all
// before the transaction opens.
func (s *NativeSessions) prepareReissue(ctx context.Context, expected NativePrincipal, current, next string) (*passwordReissue, error) {
	actual, _, err := s.authenticateNativeHash(ctx, expected.accessHash)
	if err != nil {
		return nil, err
	}
	if !expected.SameNativeAccess(actual) {
		return nil, ErrNativeUnauthorized
	}
	capturedHash, err := s.capturedPasswordHash(ctx, expected)
	if err != nil {
		return nil, err
	}
	if !CheckPassword(current, capturedHash) {
		return nil, ErrWrongPassword
	}
	newHash, err := HashPassword(next)
	if err != nil {
		return nil, nativeStoreFailure(err)
	}
	access, refresh, err := generateNativeSecrets(s.random)
	if err != nil {
		return nil, err
	}
	familyID, err := uuid.NewRandomFromReader(s.random)
	if err != nil {
		return nil, errNativeEntropy
	}
	return &passwordReissue{expected: expected, capturedHash: capturedHash, newHash: newHash, familyID: familyID, access: access, refresh: refresh}, nil
}

func (s *NativeSessions) capturedPasswordHash(ctx context.Context, expected NativePrincipal) (string, error) {
	var capturedHash string
	err := s.pool.QueryRow(ctx, `SELECT password_hash FROM users WHERE id=$1 AND token_version=$2 AND `+ActiveSQL(""), expected.user.ID, expected.tokenVersion).Scan(&capturedHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNativeUnauthorized
	}
	if err != nil {
		return "", nativeStoreFailure(err)
	}
	return capturedHash, nil
}

// reissue re-validates the session and the captured password hash under the
// user-first locks, writes the new password, revokes every family of the
// account and opens the new one.
func (s *NativeSessions) reissue(ctx context.Context, tx pgx.Tx, change *passwordReissue) error {
	expected := change.expected
	locked, err := lockNativeSession(ctx, tx, expected.user.ID, expected.familyID)
	if err != nil {
		return err
	}
	now, err := locked.checkReissueLease(ctx, tx, expected)
	if err != nil {
		return err
	}
	if err := change.checkUnchanged(ctx, tx, locked.user.ID); err != nil {
		return err
	}
	var nextVersion int
	if err = tx.QueryRow(ctx, `UPDATE users SET password_hash=$1,token_version=token_version+1,updated_at=NOW() WHERE id=$2 RETURNING token_version`, change.newHash, locked.user.ID).Scan(&nextVersion); err != nil {
		return nativeStoreFailure(err)
	}
	change.wrote = true
	return s.finishReissue(ctx, tx, change, locked, now, nextVersion)
}

// finishReissue revokes the account's families and opens the new one. The
// password row is already written, so the caller has marked the change.
func (s *NativeSessions) finishReissue(ctx context.Context, tx pgx.Tx, change *passwordReissue, locked lockedNativeSession, now time.Time, nextVersion int) error {
	expected := change.expected
	// Existing version_mismatch is the accurate reason for these old
	// families; no applied migration or revocation enum is changed.
	if _, err := tx.Exec(ctx, `UPDATE native_session_families SET revoked_at=$2,revoke_reason='version_mismatch' WHERE user_id=$1 AND revoked_at IS NULL`, locked.user.ID, now); err != nil {
		return nativeStoreFailure(err)
	}
	expiry := now.Add(s.policy.FamilyLifetime)
	if _, err := tx.Exec(ctx, `INSERT INTO native_session_families(id,user_id,client_instance_id,issued_token_version,created_at,expires_at,refresh_after) VALUES($1,$2,$3,$4,$5,$6,$7)`, change.familyID, locked.user.ID, expected.instanceID, nextVersion, now, expiry, now.Add(nativeRefreshInterval)); err != nil {
		return nativeStoreFailure(err)
	}
	accessExpiry, err := insertNativeTokens(ctx, tx, change.familyID, 0, now, expiry, change.access, change.refresh)
	if err != nil {
		return nativeStoreFailure(err)
	}
	if _, err = locked.checkReissueLease(ctx, tx, expected); err != nil {
		return err
	}
	change.result = IssuedNative{principal: NativePrincipal{user: *locked.user, familyID: change.familyID, instanceID: expected.instanceID, tokenVersion: nextVersion, accessExpiresAt: accessExpiry, familyExpiresAt: expiry, accessHash: change.access.digest()}, access: change.access, refresh: change.refresh}
	return nil
}

// checkUnchanged verifies under lock that neither the password hash nor the
// access token's expiry moved since the caller was authenticated.
func (c *passwordReissue) checkUnchanged(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	var lockedHash string
	if err := tx.QueryRow(ctx, `SELECT password_hash FROM users WHERE id=$1`, userID).Scan(&lockedHash); err != nil {
		return nativeStoreFailure(err)
	}
	if lockedHash != c.capturedHash {
		return ErrNativeUnauthorized
	}
	var accessExpiry time.Time
	if err := tx.QueryRow(ctx, `SELECT expires_at FROM native_access_tokens WHERE token_hash=$1 AND family_id=$2`, c.expected.accessHash[:], c.expected.familyID).Scan(&accessExpiry); err != nil {
		return nativeLockFailure(err)
	}
	if !accessExpiry.Equal(c.expected.accessExpiresAt) {
		return ErrNativeUnauthorized
	}
	return nil
}
