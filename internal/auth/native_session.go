package auth

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
)

const (
	nativeAccessTTL       = 5 * time.Minute
	nativeMaxFamilyTTL    = 30 * 24 * time.Hour
	nativeRefreshInterval = time.Minute
	nativeMaxFamilies     = 8
	nativeMaxSequence     = 65535
	nativeQueryTimeout    = 5 * time.Second
)

// NativePolicy is validated independently of server configuration. This slice
// has no routes and is not a replacement for browser cookie sessions.
type NativePolicy struct {
	SessionLifetime time.Duration
	FamilyLifetime  time.Duration
}

// Only the account/login policy inside this package may mint a proof. Keeping
// this private prevents a future JSON request from supplying its own version.
// No native login path exists yet; shared Handler lockouts must precede minting.
type verifiedNativeLogin struct {
	userID       uuid.UUID
	tokenVersion int
}

// NativePrincipal is transport authorization, never E2EE/device approval.
// Private fields prevent external callers from forging grant ownership.
type NativePrincipal struct {
	user            User
	familyID        uuid.UUID
	instanceID      uuid.UUID
	tokenVersion    int
	accessExpiresAt time.Time
	familyExpiresAt time.Time
}

func (p NativePrincipal) User() User                  { return p.user }
func (p NativePrincipal) FamilyID() uuid.UUID         { return p.familyID }
func (p NativePrincipal) ClientInstanceID() uuid.UUID { return p.instanceID }
func (p NativePrincipal) TokenVersion() int           { return p.tokenVersion }

// IssuedNative never marshals automatically; eventual HTTP wire conversion
// belongs only to the native broker endpoint after its own security review.
type IssuedNative struct {
	principal NativePrincipal
	access    nativeSecret
	refresh   nativeSecret
}

func (IssuedNative) Format(state fmt.State, _ rune) {
	_, _ = io.WriteString(state, "[native grant redacted]")
}

func (g IssuedNative) Principal() NativePrincipal { return g.principal }
func (g IssuedNative) AccessExpiresAt() time.Time { return g.principal.accessExpiresAt }
func (g IssuedNative) FamilyExpiresAt() time.Time { return g.principal.familyExpiresAt }
func (IssuedNative) String() string               { return "[native grant redacted]" }
func (IssuedNative) GoString() string             { return "[native grant redacted]" }
func (IssuedNative) LogValue() slog.Value         { return slog.StringValue("[native grant redacted]") }
func (IssuedNative) MarshalJSON() ([]byte, error) { return nil, errNativeSerialization }

type NativeSessions struct {
	pool   *db.Pool
	policy NativePolicy
	random io.Reader
	begin  func(context.Context) (pgx.Tx, error)
}

func NewNativeSessions(pool *db.Pool, policy NativePolicy) (*NativeSessions, error) {
	if pool == nil || pool.Pool == nil || policy.SessionLifetime < time.Hour ||
		policy.SessionLifetime > 365*24*time.Hour || policy.FamilyLifetime < time.Second ||
		policy.FamilyLifetime > nativeMaxFamilyTTL || policy.FamilyLifetime > policy.SessionLifetime {
		return nil, errors.New("invalid native session policy")
	}
	return &NativeSessions{
		pool: pool, policy: policy, random: rand.Reader,
		begin: func(ctx context.Context) (pgx.Tx, error) {
			return pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
		},
	}, nil
}

func (s *NativeSessions) transact(ctx context.Context, fn func(context.Context, pgx.Tx) error) (err error) {
	tx, beginErr := s.begin(ctx)
	if beginErr != nil {
		return nativeStoreFailure(beginErr)
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if rollbackErr := tx.Rollback(cleanup); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			err = errors.Join(err, nativeStoreFailure(rollbackErr))
		}
	}()
	if err = fn(ctx, tx); err != nil {
		return err
	}
	if commitErr := tx.Commit(ctx); commitErr != nil {
		return nativeStoreFailure(commitErr)
	}
	committed = true
	return nil
}

func lockNativeUser(ctx context.Context, tx pgx.Tx, id uuid.UUID, skipLocked bool) (*User, int, bool, error) {
	query := `SELECT ` + userColumns + `, token_version, disabled_at IS NOT NULL FROM users WHERE id=$1 FOR UPDATE`
	if skipLocked {
		query += ` SKIP LOCKED`
	}
	var version int
	var disabled bool
	user, err := scanUser(tx.QueryRow(ctx, query, id), &version, &disabled)
	return user, version, disabled, err
}

type nativeFamily struct {
	instanceID   uuid.UUID
	version      int
	expiresAt    time.Time
	refreshAfter time.Time
	revokedAt    *time.Time
}

func lockNativeFamily(ctx context.Context, tx pgx.Tx, familyID, userID uuid.UUID, skipLocked bool) (nativeFamily, error) {
	query := `SELECT client_instance_id, issued_token_version, expires_at, refresh_after, revoked_at
		FROM native_session_families WHERE id=$1 AND user_id=$2 FOR UPDATE`
	if skipLocked {
		query += ` SKIP LOCKED`
	}
	var family nativeFamily
	err := tx.QueryRow(ctx, query, familyID, userID).Scan(&family.instanceID, &family.version,
		&family.expiresAt, &family.refreshAfter, &family.revokedAt)
	return family, err
}

func nativeClock(ctx context.Context, tx pgx.Tx) (time.Time, error) {
	var now time.Time
	err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now)
	return now, err
}

func revokeNativeFamily(ctx context.Context, tx pgx.Tx, familyID uuid.UUID, now time.Time, reason string) error {
	_, err := tx.Exec(ctx, `UPDATE native_session_families SET revoked_at=$2, revoke_reason=$3
		WHERE id=$1 AND revoked_at IS NULL`, familyID, now, reason)
	return err
}

func insertNativeTokens(ctx context.Context, tx pgx.Tx, familyID uuid.UUID, sequence int64,
	now, familyExpiry time.Time, access, refresh nativeSecret) (time.Time, error) {
	accessExpiry := now.Add(nativeAccessTTL)
	if familyExpiry.Before(accessExpiry) {
		accessExpiry = familyExpiry
	}
	accessHash, refreshHash := access.digest(), refresh.digest()
	if _, err := tx.Exec(ctx, `INSERT INTO native_access_tokens(token_hash,family_id,created_at,expires_at)
		VALUES($1,$2,$3,$4)`, accessHash[:], familyID, now, accessExpiry); err != nil {
		return time.Time{}, err
	}
	_, err := tx.Exec(ctx, `INSERT INTO native_refresh_tokens(token_hash,family_id,sequence,created_at,expires_at)
		VALUES($1,$2,$3,$4,$5)`, refreshHash[:], familyID, sequence, now, familyExpiry)
	return accessExpiry, err
}

func (s *NativeSessions) IssueVerified(ctx context.Context, proof verifiedNativeLogin, instanceID uuid.UUID) (IssuedNative, error) {
	if proof.userID == uuid.Nil || proof.tokenVersion < 0 || instanceID == uuid.Nil {
		return IssuedNative{}, ErrNativeUnauthorized
	}
	ctx, cancel := context.WithTimeout(ctx, nativeQueryTimeout)
	defer cancel()
	access, refresh, err := generateNativeSecrets(s.random)
	if err != nil {
		return IssuedNative{}, err
	}
	familyID, err := uuid.NewRandomFromReader(s.random)
	if err != nil {
		return IssuedNative{}, errNativeEntropy
	}
	var result IssuedNative
	err = s.transact(ctx, func(ctx context.Context, tx pgx.Tx) error {
		user, version, disabled, err := lockNativeUser(ctx, tx, proof.userID, false)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNativeUnauthorized
		}
		if err != nil {
			return nativeStoreFailure(err)
		}
		if disabled || version != proof.tokenVersion {
			return ErrNativeUnauthorized
		}
		now, err := nativeClock(ctx, tx)
		if err != nil {
			return nativeStoreFailure(err)
		}
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM native_session_families WHERE user_id=$1
			AND issued_token_version=$2 AND revoked_at IS NULL AND expires_at>$3`, user.ID, version, now).Scan(&count); err != nil {
			return nativeStoreFailure(err)
		}
		if count >= nativeMaxFamilies {
			return ErrNativeLimit
		}
		expiry := now.Add(s.policy.FamilyLifetime)
		if _, err := tx.Exec(ctx, `INSERT INTO native_session_families
			(id,user_id,client_instance_id,issued_token_version,created_at,expires_at,refresh_after)
			VALUES($1,$2,$3,$4,$5,$6,$7)`, familyID, user.ID, instanceID, version, now, expiry, now.Add(nativeRefreshInterval)); err != nil {
			return nativeStoreFailure(err)
		}
		accessExpiry, err := insertNativeTokens(ctx, tx, familyID, 0, now, expiry, access, refresh)
		if err != nil {
			return nativeStoreFailure(err)
		}
		result = IssuedNative{principal: NativePrincipal{user: *user, familyID: familyID, instanceID: instanceID,
			tokenVersion: version, accessExpiresAt: accessExpiry, familyExpiresAt: expiry}, access: access, refresh: refresh}
		return nil
	})
	if err != nil {
		return IssuedNative{}, err
	}
	return result, nil
}

func (s *NativeSessions) AuthenticateAccess(ctx context.Context, encoded string) (NativePrincipal, error) {
	secret, err := parseNativeSecret(encoded)
	if err != nil {
		return NativePrincipal{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, nativeQueryTimeout)
	defer cancel()
	hash := secret.digest()
	var principal NativePrincipal
	user, err := scanUser(s.pool.QueryRow(ctx, `SELECT u.id,u.username,u.display_name,u.bio,u.role,
		u.avatar_s3_key,u.status_text,u.presence,u.locale,u.created_at,
		f.id,f.client_instance_id,f.issued_token_version,a.expires_at,f.expires_at
		FROM native_access_tokens a JOIN native_session_families f ON f.id=a.family_id
		JOIN users u ON u.id=f.user_id WHERE a.token_hash=$1
		AND a.expires_at>clock_timestamp() AND f.expires_at>clock_timestamp()
		AND f.revoked_at IS NULL AND u.disabled_at IS NULL AND f.issued_token_version=u.token_version`, hash[:]),
		&principal.familyID, &principal.instanceID, &principal.tokenVersion, &principal.accessExpiresAt, &principal.familyExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return NativePrincipal{}, ErrNativeUnauthorized
	}
	if err != nil {
		return NativePrincipal{}, nativeStoreFailure(err)
	}
	principal.user = *user
	return principal, nil
}

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
			tokenVersion: version, accessExpiresAt: accessExpiry, familyExpiresAt: family.expiresAt}, access: access, refresh: refresh}
		return nil
	})
	if err != nil {
		return IssuedNative{}, err
	}
	if outcome != nil {
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
			OR f.issued_token_version<>u.token_version OR u.disabled_at IS NOT NULL
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
