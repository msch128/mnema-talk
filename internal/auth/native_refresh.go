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
	var result IssuedNative
	var outcome error
	err = s.transact(ctx, func(ctx context.Context, tx pgx.Tx) error {
		user, version, disabled, err := lockNativeUser(ctx, tx, userID, false)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNativeUnauthorized
		}
		if err != nil {
			return nativeStoreFailure(err)
		}
		family, err := lockNativeFamily(ctx, tx, familyID, userID, false)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNativeUnauthorized
		}
		if err != nil {
			return nativeStoreFailure(err)
		}
		var sequence int64
		var expiry time.Time
		var consumed *time.Time
		if err := tx.QueryRow(ctx, `SELECT sequence,expires_at,consumed_at FROM native_refresh_tokens
			WHERE token_hash=$1 AND family_id=$2 FOR UPDATE`, hash[:], familyID).Scan(&sequence, &expiry, &consumed); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNativeUnauthorized
			}
			return nativeStoreFailure(err)
		}
		now, err := nativeClock(ctx, tx)
		if err != nil {
			return nativeStoreFailure(err)
		}
		if disabled || family.revokedAt != nil || !now.Before(family.expiresAt) {
			return ErrNativeUnauthorized
		}
		reason := ""
		if version != family.version {
			reason = "version_mismatch"
		} else if consumed != nil {
			reason = "refresh_reuse"
		}
		if reason == "" && sequence >= nativeMaxSequence {
			reason = "rotation_limit"
		}
		if reason != "" {
			if err := revokeNativeFamily(ctx, tx, familyID, now, reason); err != nil {
				return nativeStoreFailure(err)
			}
			// The auth error is an outcome AFTER commit, not a callback error
			// that would roll the security transition back.
			outcome = ErrNativeUnauthorized
			return nil
		}
		if !expiry.Equal(family.expiresAt) || !now.Before(expiry) {
			return ErrNativeUnauthorized
		}
		if now.Before(family.refreshAfter) {
			return ErrNativeRefreshWait
		}
		access, refresh, err := generateNativeSecrets(s.random)
		if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `UPDATE native_refresh_tokens SET consumed_at=$2
			WHERE token_hash=$1 AND consumed_at IS NULL`, hash[:], now)
		if err != nil {
			return nativeStoreFailure(err)
		}
		if tag.RowsAffected() != 1 {
			return ErrNativeUnauthorized
		}
		accessExpiry, err := insertNativeTokens(ctx, tx, familyID, sequence+1, now, family.expiresAt, access, refresh)
		if err != nil {
			return nativeStoreFailure(err)
		}
		if _, err := tx.Exec(ctx, `UPDATE native_session_families SET refresh_after=$2 WHERE id=$1`, familyID, now.Add(nativeRefreshInterval)); err != nil {
			return nativeStoreFailure(err)
		}
		result = IssuedNative{principal: NativePrincipal{user: *user, familyID: familyID, instanceID: family.instanceID,
			tokenVersion: version, accessExpiresAt: accessExpiry, familyExpiresAt: family.expiresAt, accessHash: access.digest()}, access: access, refresh: refresh, refreshSequence: uint64(sequence + 1)}
		return nil
	})
	if err != nil {
		return IssuedNative{}, err
	}
	if outcome != nil {
		s.disconnectNativeFamily(familyID)
		return IssuedNative{}, outcome
	}
	return result, nil
}

func (s *NativeSessions) RevokeFamily(ctx context.Context, principal NativePrincipal) error {
	if principal.user.ID == uuid.Nil || principal.familyID == uuid.Nil || principal.instanceID == uuid.Nil {
		return ErrNativeUnauthorized
	}
	ctx, cancel := context.WithTimeout(ctx, nativeQueryTimeout)
	defer cancel()
	err := s.transact(ctx, func(ctx context.Context, tx pgx.Tx) error {
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
		now, err := nativeClock(ctx, tx)
		if err != nil {
			return nativeStoreFailure(err)
		}
		if disabled || version != principal.tokenVersion || family.version != version ||
			family.instanceID != principal.instanceID || !now.Before(principal.accessExpiresAt) || !now.Before(family.expiresAt) {
			return ErrNativeUnauthorized
		}
		if err := revokeNativeFamily(ctx, tx, principal.familyID, now, "logout"); err != nil {
			return nativeStoreFailure(err)
		}
		return nil
	})
	if err == nil {
		s.disconnectNativeFamily(principal.familyID)
	}
	return err
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
		rows, err := tx.Query(ctx, `SELECT f.id,f.user_id FROM native_session_families f JOIN users u ON u.id=f.user_id
			WHERE f.revoked_at IS NOT NULL OR f.expires_at<=clock_timestamp()
			OR f.issued_token_version<>u.token_version OR `+InactiveSQL("u")+`
			ORDER BY f.user_id,f.id LIMIT $1`, batch)
		if err != nil {
			return nativeStoreFailure(err)
		}
		type locator struct{ familyID, userID uuid.UUID }
		var locators []locator
		for rows.Next() {
			var item locator
			if err := rows.Scan(&item.familyID, &item.userID); err != nil {
				rows.Close()
				return nativeStoreFailure(err)
			}
			locators = append(locators, item)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nativeStoreFailure(err)
		}
		for _, item := range locators {
			_, version, disabled, err := lockNativeUser(ctx, tx, item.userID, true)
			if errors.Is(err, pgx.ErrNoRows) {
				continue
			}
			if err != nil {
				return nativeStoreFailure(err)
			}
			family, err := lockNativeFamily(ctx, tx, item.familyID, item.userID, true)
			if errors.Is(err, pgx.ErrNoRows) {
				continue
			}
			if err != nil {
				return nativeStoreFailure(err)
			}
			now, err := nativeClock(ctx, tx)
			if err != nil {
				return nativeStoreFailure(err)
			}
			if family.revokedAt == nil && now.Before(family.expiresAt) && version == family.version && !disabled {
				continue
			}
			tag, err := tx.Exec(ctx, `DELETE FROM native_session_families WHERE id=$1`, item.familyID)
			if err != nil {
				return nativeStoreFailure(err)
			}
			deleted += int(tag.RowsAffected())
		}
		if deleted < batch {
			tag, err := tx.Exec(ctx, `DELETE FROM native_access_tokens WHERE token_hash IN
				(SELECT token_hash FROM native_access_tokens WHERE expires_at<=clock_timestamp()
				ORDER BY expires_at,token_hash LIMIT $1 FOR UPDATE SKIP LOCKED)`, batch-deleted)
			if err != nil {
				return nativeStoreFailure(err)
			}
			deleted += int(tag.RowsAffected())
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return deleted, nil
}
