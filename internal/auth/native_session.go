package auth

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
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
	accessHash      [32]byte
}

func (p NativePrincipal) User() User { return p.user }

func (p NativePrincipal) FamilyID() uuid.UUID { return p.familyID }

func (p NativePrincipal) ClientInstanceID() uuid.UUID { return p.instanceID }

func (p NativePrincipal) TokenVersion() int { return p.tokenVersion }

// IssuedNative never marshals automatically; eventual HTTP wire conversion
// belongs only to the native broker endpoint after its own security review.
type IssuedNative struct {
	principal       NativePrincipal
	access          nativeSecret
	refresh         nativeSecret
	refreshSequence uint64
}

func (IssuedNative) Format(state fmt.State, _ rune) {
	writeNativeText(state, "[native grant redacted]")
}

func (g IssuedNative) RefreshSequence() uint64 { return g.refreshSequence }

func (g IssuedNative) Principal() NativePrincipal { return g.principal }

func (g IssuedNative) AccessExpiresAt() time.Time { return g.principal.accessExpiresAt }

func (g IssuedNative) FamilyExpiresAt() time.Time { return g.principal.familyExpiresAt }

func (IssuedNative) String() string { return "[native grant redacted]" }

func (IssuedNative) GoString() string { return "[native grant redacted]" }

func (IssuedNative) LogValue() slog.Value { return slog.StringValue("[native grant redacted]") }

func (IssuedNative) MarshalJSON() ([]byte, error) { return nil, errNativeSerialization }

type NativeSessions struct {
	pool            *db.Pool
	policy          NativePolicy
	random          io.Reader
	begin           func(context.Context) (pgx.Tx, error)
	familyControlMu sync.RWMutex
	familyControl   NativeFamilyControl
}

func invalidNativePolicy(pool *db.Pool, policy NativePolicy) bool {
	if pool == nil || pool.Pool == nil {
		return true
	}
	if policy.SessionLifetime < time.Hour || policy.SessionLifetime > 365*24*time.Hour {
		return true
	}
	if policy.FamilyLifetime < time.Second || policy.FamilyLifetime > nativeMaxFamilyTTL {
		return true
	}
	return policy.FamilyLifetime > policy.SessionLifetime
}

func readCommitted(pool *db.Pool) func(context.Context) (pgx.Tx, error) {
	return func(ctx context.Context) (pgx.Tx, error) {
		return pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	}
}

func NewNativeSessions(pool *db.Pool, policy NativePolicy) (*NativeSessions, error) {
	if invalidNativePolicy(pool, policy) {
		return nil, errors.New("invalid native session policy")
	}
	return &NativeSessions{pool: pool, policy: policy, random: rand.Reader, begin: readCommitted(pool)}, nil
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
	query := `SELECT ` + userColumns + `, token_version, ` + InactiveSQL("") + ` FROM users WHERE id=$1 FOR UPDATE`
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
		var err error
		result, err = s.openFamily(ctx, tx, proof, instanceID, familyID, access, refresh)
		return err
	})
	if err != nil {
		return IssuedNative{}, err
	}
	return result, nil
}

// openFamily stores a new initial family for the verified login, unless the
// account changed since or already holds the maximum number of live families.
func (s *NativeSessions) openFamily(ctx context.Context, tx pgx.Tx, proof verifiedNativeLogin, instanceID, familyID uuid.UUID, access, refresh nativeSecret) (IssuedNative, error) {
	user, version, disabled, err := lockNativeUser(ctx, tx, proof.userID, false)
	if err != nil {
		return IssuedNative{}, nativeLockFailure(err)
	}
	if disabled || version != proof.tokenVersion {
		return IssuedNative{}, ErrNativeUnauthorized
	}
	now, err := nativeClock(ctx, tx)
	if err != nil {
		return IssuedNative{}, nativeStoreFailure(err)
	}
	var count int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM native_session_families WHERE user_id=$1
		AND issued_token_version=$2 AND revoked_at IS NULL AND expires_at>$3`, user.ID, version, now).Scan(&count); err != nil {
		return IssuedNative{}, nativeStoreFailure(err)
	}
	if count >= nativeMaxFamilies {
		return IssuedNative{}, ErrNativeLimit
	}
	return s.storeOpenedFamily(ctx, tx, user, version, instanceID, familyID, now, access, refresh)
}

func (s *NativeSessions) storeOpenedFamily(ctx context.Context, tx pgx.Tx, user *User, version int, instanceID, familyID uuid.UUID, now time.Time, access, refresh nativeSecret) (IssuedNative, error) {
	expiry := now.Add(s.policy.FamilyLifetime)
	if _, err := tx.Exec(ctx, `INSERT INTO native_session_families
		(id,user_id,client_instance_id,issued_token_version,created_at,expires_at,refresh_after)
		VALUES($1,$2,$3,$4,$5,$6,$7)`, familyID, user.ID, instanceID, version, now, expiry, now.Add(nativeRefreshInterval)); err != nil {
		return IssuedNative{}, nativeStoreFailure(err)
	}
	accessExpiry, err := insertNativeTokens(ctx, tx, familyID, 0, now, expiry, access, refresh)
	if err != nil {
		return IssuedNative{}, nativeStoreFailure(err)
	}
	return IssuedNative{principal: NativePrincipal{user: *user, familyID: familyID, instanceID: instanceID,
		tokenVersion: version, accessExpiresAt: accessExpiry, familyExpiresAt: expiry, accessHash: access.digest()}, access: access, refresh: refresh}, nil
}

func (s *NativeSessions) AuthenticateAccess(ctx context.Context, encoded string) (NativePrincipal, error) {
	secret, err := parseNativeSecret(encoded)
	if err != nil {
		return NativePrincipal{}, err
	}
	principal, _, err := s.authenticateNativeHash(ctx, secret.digest())
	return principal, err
}
