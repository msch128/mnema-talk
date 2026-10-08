package auth

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const NativeLeaseMaximum = 15 * time.Second
const nativeLeaseQueryTimeout = 2 * time.Second

// NativeLease is an auth-owned, bounded transport capability. It contains no
// raw bearer. Monotonic access/family deadlines must not move during rechecks.
type NativeLease struct {
	principal      NativePrincipal
	deadline       time.Time
	accessDeadline time.Time
	familyDeadline time.Time
}

func (l NativeLease) Principal() NativePrincipal { return l.principal }
func (l NativeLease) Deadline() time.Time        { return l.deadline }
func (l NativeLease) AccessDeadline() time.Time  { return l.accessDeadline }
func (l NativeLease) FamilyDeadline() time.Time  { return l.familyDeadline }

func (p NativePrincipal) SameNativeBinding(other NativePrincipal) bool {
	return p.user.ID != uuid.Nil && p.familyID != uuid.Nil && p.instanceID != uuid.Nil &&
		p.user.ID == other.user.ID && p.familyID == other.familyID && p.instanceID == other.instanceID &&
		p.tokenVersion == other.tokenVersion && p.familyExpiresAt.Equal(other.familyExpiresAt)
}

func (p NativePrincipal) SameNativeAccess(other NativePrincipal) bool {
	return p.SameNativeBinding(other) && p.accessHash == other.accessHash && p.accessExpiresAt.Equal(other.accessExpiresAt)
}

func (s *NativeSessions) authenticateNativeHash(ctx context.Context, hash [32]byte) (NativePrincipal, time.Time, error) {
	ctx, cancel := context.WithTimeout(ctx, nativeQueryTimeout)
	defer cancel()
	var principal NativePrincipal
	var databaseNow time.Time
	user, err := scanUser(s.pool.QueryRow(ctx, `SELECT u.id,u.username,u.display_name,u.bio,u.role,
		u.avatar_s3_key,u.status_text,u.presence,u.locale,u.created_at,
		f.id,f.client_instance_id,f.issued_token_version,a.expires_at,f.expires_at,clock_timestamp()
		FROM native_access_tokens a JOIN native_session_families f ON f.id=a.family_id
		JOIN users u ON u.id=f.user_id WHERE a.token_hash=$1
		AND a.expires_at>clock_timestamp() AND f.expires_at>clock_timestamp()
		AND f.revoked_at IS NULL AND u.disabled_at IS NULL AND f.issued_token_version=u.token_version`, hash[:]),
		&principal.familyID, &principal.instanceID, &principal.tokenVersion, &principal.accessExpiresAt, &principal.familyExpiresAt, &databaseNow)
	if errors.Is(err, pgx.ErrNoRows) {
		return NativePrincipal{}, time.Time{}, ErrNativeUnauthorized
	}
	if err != nil {
		return NativePrincipal{}, time.Time{}, nativeStoreFailure(err)
	}
	principal.user = *user
	principal.accessHash = hash
	return principal, databaseNow, nil
}

func makeNativeLease(start, databaseNow time.Time, p NativePrincipal) (NativeLease, error) {
	accessRemaining := min(nativeAccessTTL, p.accessExpiresAt.Sub(databaseNow))
	familyRemaining := min(nativeMaxFamilyTTL, p.familyExpiresAt.Sub(databaseNow))
	deadline := start.Add(min(NativeLeaseMaximum, accessRemaining, familyRemaining))
	if !time.Now().Before(deadline) {
		return NativeLease{}, ErrNativeUnauthorized
	}
	return NativeLease{principal: p, deadline: deadline, accessDeadline: start.Add(accessRemaining), familyDeadline: start.Add(familyRemaining)}, nil
}

func (s *NativeSessions) AuthenticateAccessLease(ctx context.Context, encoded string) (NativeLease, error) {
	secret, err := parseNativeSecret(encoded)
	if err != nil {
		return NativeLease{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, nativeLeaseQueryTimeout)
	defer cancel()
	start := time.Now()
	p, now, err := s.authenticateNativeHash(ctx, secret.digest())
	if err != nil {
		return NativeLease{}, err
	}
	return makeNativeLease(start, now, p)
}

func (s *NativeSessions) RevalidateNativeLease(ctx context.Context, expected NativePrincipal) (NativeLease, error) {
	if expected.user.ID == uuid.Nil || expected.familyID == uuid.Nil || expected.instanceID == uuid.Nil {
		return NativeLease{}, ErrNativeUnauthorized
	}
	ctx, cancel := context.WithTimeout(ctx, nativeLeaseQueryTimeout)
	defer cancel()
	start := time.Now()
	p, now, err := s.authenticateNativeHash(ctx, expected.accessHash)
	if err != nil {
		return NativeLease{}, err
	}
	if !expected.SameNativeAccess(p) {
		return NativeLease{}, ErrNativeUnauthorized
	}
	return makeNativeLease(start, now, p)
}

// NativeFamilyControl receives confirmed committed family revocation metadata.
// Binding is server-owned, one-time, and never set from a renderer or request.
type NativeFamilyControl interface{ DisconnectNativeFamily(uuid.UUID) }

func (s *NativeSessions) BindNativeFamilyControl(control NativeFamilyControl) error {
	s.familyControlMu.Lock()
	defer s.familyControlMu.Unlock()
	if control == nil || s.familyControl != nil {
		return errors.New("invalid native family control binding")
	}
	s.familyControl = control
	return nil
}

func (s *NativeSessions) disconnectNativeFamily(id uuid.UUID) {
	s.familyControlMu.RLock()
	control := s.familyControl
	s.familyControlMu.RUnlock()
	if control != nil {
		control.DisconnectNativeFamily(id)
	}
}

func (p NativePrincipal) AccessExpiresAt() time.Time { return p.accessExpiresAt }
func (p NativePrincipal) FamilyExpiresAt() time.Time { return p.familyExpiresAt }
