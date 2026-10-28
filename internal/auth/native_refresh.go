package auth

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (s *NativeSessions) RotateRefresh(ctx context.Context, encoded string) (IssuedNative, error) {
	secret, err := parseNativeSecret(encoded)
	if err != nil {
		return IssuedNative{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, nativeQueryTimeout)
	defer cancel()
	hash := secret.digest()
	var familyID, userID uuid.UUID
	err = s.pool.QueryRow(ctx, `SELECT r.family_id,f.user_id FROM native_refresh_tokens r
		JOIN native_session_families f ON f.id=r.family_id WHERE r.token_hash=$1`, hash[:]).Scan(&familyID, &userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return IssuedNative{}, ErrNativeUnauthorized
	}
	if err != nil {
		return IssuedNative{}, nativeStoreFailure(err)
	}
	var rotation nativeRotation
	err = s.transact(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		rotation, err = s.rotate(ctx, tx, hash, familyID, userID)
		return err
	})
	if err != nil {
		return IssuedNative{}, err
	}
	if rotation.familyRevoked {
		s.disconnectNativeFamily(familyID)
		return IssuedNative{}, ErrNativeUnauthorized
	}
	return rotation.issued, nil
}

// nativeRotation is what a refresh attempt committed: either the successor
// tokens or the revocation of the whole family.
type nativeRotation struct {
	issued IssuedNative
	// familyRevoked reports a committed revocation. The unauthorized error is
	// an outcome AFTER commit, not a callback error that would roll the
	// security transition back.
	familyRevoked bool
}

// refreshToken is the stored state of a presented refresh token.
type refreshToken struct {
	sequence int64
	expiry   time.Time
	consumed *time.Time
}

// refreshWaitError is ErrNativeRefreshWait with the time left until the family
// may refresh again.
type refreshWaitError struct{ retry time.Duration }

func (refreshWaitError) Error() string        { return ErrNativeRefreshWait.Error() }
func (refreshWaitError) Is(target error) bool { return target == ErrNativeRefreshWait }

// nativeRefreshRetry is how long a client must wait after err, a refresh
// refusal: the remaining time when known, else the full refresh interval.
func nativeRefreshRetry(err error) time.Duration {
	var wait refreshWaitError
	if errors.As(err, &wait) && wait.retry > 0 {
		return wait.retry
	}
	return nativeRefreshInterval
}

// rotate redeems a refresh token under the user-first locks. A token that
// shows reuse, a version mismatch or an exhausted rotation budget revokes the
// whole family instead.
func (s *NativeSessions) rotate(ctx context.Context, tx pgx.Tx, hash [32]byte, familyID, userID uuid.UUID) (nativeRotation, error) {
	locked, err := lockNativeSession(ctx, tx, userID, familyID)
	if err != nil {
		return nativeRotation{}, err
	}
	token, err := lockedRefreshToken(ctx, tx, hash, familyID)
	if err != nil {
		return nativeRotation{}, err
	}
	now, err := nativeClock(ctx, tx)
	if err != nil {
		return nativeRotation{}, nativeStoreFailure(err)
	}
	if locked.closed(now) {
		return nativeRotation{}, ErrNativeUnauthorized
	}
	if reason := refreshRevocationReason(locked, token); reason != "" {
		return revokePresentedFamily(ctx, tx, familyID, now, reason)
	}
	family := locked.family
	if refreshTokenStale(token, family.expiresAt, now) {
		return nativeRotation{}, ErrNativeUnauthorized
	}
	if now.Before(family.refreshAfter) {
		return nativeRotation{}, refreshWaitError{retry: family.refreshAfter.Sub(now)}
	}
	issued, err := s.issueSuccessor(ctx, tx, hash, locked, familyID, token.sequence, now)
	return nativeRotation{issued: issued}, err
}

func lockedRefreshToken(ctx context.Context, tx pgx.Tx, hash [32]byte, familyID uuid.UUID) (refreshToken, error) {
	var token refreshToken
	err := tx.QueryRow(ctx, `SELECT sequence,expires_at,consumed_at FROM native_refresh_tokens
		WHERE token_hash=$1 AND family_id=$2 FOR UPDATE`, hash[:], familyID).Scan(&token.sequence, &token.expiry, &token.consumed)
	if err != nil {
		return refreshToken{}, nativeLockFailure(err)
	}
	return token, nil
}

func refreshTokenStale(token refreshToken, expiresAt, now time.Time) bool {
	if !token.expiry.Equal(expiresAt) {
		return true
	}
	return !now.Before(token.expiry)
}

func revokePresentedFamily(ctx context.Context, tx pgx.Tx, familyID uuid.UUID, now time.Time, reason string) (nativeRotation, error) {
	if err := revokeNativeFamily(ctx, tx, familyID, now, reason); err != nil {
		return nativeRotation{}, nativeStoreFailure(err)
	}
	return nativeRotation{familyRevoked: true}, nil
}

// refreshRevocationReason names why a presented refresh token must revoke its
// family, or is empty when it is a legitimate next rotation.
func refreshRevocationReason(locked lockedNativeSession, token refreshToken) string {
	switch {
	case locked.version != locked.family.version:
		return "version_mismatch"
	case token.consumed != nil:
		return "refresh_reuse"
	case token.sequence >= nativeMaxSequence:
		return "rotation_limit"
	}
	return ""
}

// issueSuccessor consumes the presented refresh token and stores the next
// access/refresh pair of the family.
func (s *NativeSessions) issueSuccessor(ctx context.Context, tx pgx.Tx, hash [32]byte, locked lockedNativeSession, familyID uuid.UUID, sequence int64, now time.Time) (IssuedNative, error) {
	access, refresh, err := generateNativeSecrets(s.random)
	if err != nil {
		return IssuedNative{}, err
	}
	tag, err := tx.Exec(ctx, `UPDATE native_refresh_tokens SET consumed_at=$2
		WHERE token_hash=$1 AND consumed_at IS NULL`, hash[:], now)
	if err != nil {
		return IssuedNative{}, nativeStoreFailure(err)
	}
	if tag.RowsAffected() != 1 {
		return IssuedNative{}, ErrNativeUnauthorized
	}
	family := locked.family
	accessExpiry, err := insertNativeTokens(ctx, tx, familyID, sequence+1, now, family.expiresAt, access, refresh)
	if err != nil {
		return IssuedNative{}, nativeStoreFailure(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE native_session_families SET refresh_after=$2 WHERE id=$1`, familyID, now.Add(nativeRefreshInterval)); err != nil {
		return IssuedNative{}, nativeStoreFailure(err)
	}
	return IssuedNative{principal: NativePrincipal{user: *locked.user, familyID: familyID, instanceID: family.instanceID,
		tokenVersion: locked.version, accessExpiresAt: accessExpiry, familyExpiresAt: family.expiresAt, accessHash: access.digest()}, access: access, refresh: refresh, refreshSequence: uint64(sequence + 1)}, nil
}

func (s *NativeSessions) RevokeFamily(ctx context.Context, principal NativePrincipal) error {
	if principal.user.ID == uuid.Nil || principal.familyID == uuid.Nil || principal.instanceID == uuid.Nil {
		return ErrNativeUnauthorized
	}
	ctx, cancel := context.WithTimeout(ctx, nativeQueryTimeout)
	defer cancel()
	err := s.transact(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return revokeAuthenticatedFamily(ctx, tx, principal)
	})
	if err == nil {
		s.disconnectNativeFamily(principal.familyID)
	}
	return err
}

// revokeAuthenticatedFamily logs a session out, provided the principal still
// matches the locked account and family.
func revokeAuthenticatedFamily(ctx context.Context, tx pgx.Tx, principal NativePrincipal) error {
	locked, err := lockNativeSession(ctx, tx, principal.user.ID, principal.familyID)
	if err != nil {
		return err
	}
	now, err := nativeClock(ctx, tx)
	if err != nil {
		return nativeStoreFailure(err)
	}
	if locked.logoutRejected(now, principal) {
		return ErrNativeUnauthorized
	}
	if err := revokeNativeFamily(ctx, tx, principal.familyID, now, "logout"); err != nil {
		return nativeStoreFailure(err)
	}
	return nil
}

// Cleanup counts deleted family/access rows, not cascaded child rows. Consumed
// refresh hashes in live families are retained for the full family lifetime.
// Families are always locked user-first, including cleanup. No retention
// scheduler or media policy is introduced by this persistence-only service.
func (s *NativeSessions) Cleanup(ctx context.Context, batch int) (int, error) {
	if batch < 1 || batch > 1000 {
		return 0, errors.New("invalid native cleanup batch")
	}
	ctx, cancel := context.WithTimeout(ctx, nativeQueryTimeout)
	defer cancel()
	deleted := 0
	err := s.transact(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		deleted, err = cleanupNativeBatch(ctx, tx, batch)
		return err
	})
	if err != nil {
		return 0, err
	}
	return deleted, nil
}

type familyLocator struct{ familyID, userID uuid.UUID }

// cleanupNativeBatch deletes dead families and, with the budget left, expired
// access tokens. It returns how many rows it deleted.
func cleanupNativeBatch(ctx context.Context, tx pgx.Tx, batch int) (int, error) {
	locators, err := deadFamilyCandidates(ctx, tx, batch)
	if err != nil {
		return 0, err
	}
	deleted := 0
	for _, item := range locators {
		n, err := deleteFamilyIfDead(ctx, tx, item)
		if err != nil {
			return 0, err
		}
		deleted += n
	}
	if deleted < batch {
		tag, err := tx.Exec(ctx, `DELETE FROM native_access_tokens WHERE token_hash IN
			(SELECT token_hash FROM native_access_tokens WHERE expires_at<=clock_timestamp()
			ORDER BY expires_at,token_hash LIMIT $1 FOR UPDATE SKIP LOCKED)`, batch-deleted)
		if err != nil {
			return 0, nativeStoreFailure(err)
		}
		deleted += int(tag.RowsAffected())
	}
	return deleted, nil
}

// deadFamilyCandidates lists families that look revoked, expired or outdated;
// each is re-checked under its locks before deletion.
func deadFamilyCandidates(ctx context.Context, tx pgx.Tx, batch int) ([]familyLocator, error) {
	rows, err := tx.Query(ctx, `SELECT f.id,f.user_id FROM native_session_families f JOIN users u ON u.id=f.user_id
		WHERE f.revoked_at IS NOT NULL OR f.expires_at<=clock_timestamp()
		OR f.issued_token_version<>u.token_version OR `+InactiveSQL("u")+`
		ORDER BY f.user_id,f.id LIMIT $1`, batch)
	if err != nil {
		return nil, nativeStoreFailure(err)
	}
	var locators []familyLocator
	for rows.Next() {
		var item familyLocator
		if err := rows.Scan(&item.familyID, &item.userID); err != nil {
			rows.Close()
			return nil, nativeStoreFailure(err)
		}
		locators = append(locators, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nativeStoreFailure(err)
	}
	return locators, nil
}

func familyStillLive(family nativeFamily, version int, disabled bool, now time.Time) bool {
	if family.revokedAt != nil {
		return false
	}
	if !now.Before(family.expiresAt) {
		return false
	}
	if version != family.version {
		return false
	}
	return !disabled
}

// deleteFamilyIfDead locks the account and family (skipping rows other
// transactions hold) and deletes the family when it is revoked, expired,
// outdated or its account is disabled. It returns the rows deleted.
func deleteFamilyIfDead(ctx context.Context, tx pgx.Tx, item familyLocator) (int, error) {
	_, version, disabled, err := lockNativeUser(ctx, tx, item.userID, true)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, nativeStoreFailure(err)
	}
	family, err := lockNativeFamily(ctx, tx, item.familyID, item.userID, true)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, nativeStoreFailure(err)
	}
	now, err := nativeClock(ctx, tx)
	if err != nil {
		return 0, nativeStoreFailure(err)
	}
	if familyStillLive(family, version, disabled, now) {
		return 0, nil
	}
	tag, err := tx.Exec(ctx, `DELETE FROM native_session_families WHERE id=$1`, item.familyID)
	if err != nil {
		return 0, nativeStoreFailure(err)
	}
	return int(tag.RowsAffected()), nil
}
