//go:build integration

package auth

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/db"
)

func nativeFixture(t *testing.T) (*db.Pool, *User, *NativeSessions, verifiedNativeLogin) {
	t.Helper()
	pool, _, user := authFixture(t)
	_, version, err := Login(context.Background(), pool, user.Username, testPassword)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewNativeSessions(pool, NativePolicy{SessionLifetime: nativeMaxFamilyTTL, FamilyLifetime: nativeMaxFamilyTTL})
	if err != nil {
		t.Fatal(err)
	}
	return pool, user, service, verifiedNativeLogin{userID: user.ID, tokenVersion: version}
}

func nativeIssue(t *testing.T, service *NativeSessions, proof verifiedNativeLogin) IssuedNative {
	t.Helper()
	grant, err := service.IssueVerified(context.Background(), proof, uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	return grant
}

func nativeReady(t *testing.T, pool *db.Pool, grant IssuedNative) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `UPDATE native_session_families SET refresh_after=created_at WHERE id=$1`, grant.principal.familyID); err != nil {
		t.Fatal(err)
	}
}

func nativeCounts(t *testing.T, pool *db.Pool, expectedFamily, expectedAccess, expectedRefresh int) {
	t.Helper()
	var family, access, refresh int
	err := pool.QueryRow(context.Background(), `SELECT
		(SELECT count(*) FROM native_session_families),
		(SELECT count(*) FROM native_access_tokens),
		(SELECT count(*) FROM native_refresh_tokens)`).Scan(&family, &access, &refresh)
	if err != nil {
		t.Fatal(err)
	}
	if family != expectedFamily || access != expectedAccess || refresh != expectedRefresh {
		t.Fatalf("rows family/access/refresh=%d/%d/%d expected=%d/%d/%d", family, access, refresh, expectedFamily, expectedAccess, expectedRefresh)
	}
}

func nativeRevoked(t *testing.T, pool *db.Pool, grant IssuedNative, reason string) {
	t.Helper()
	var actual string
	var revoked bool
	if err := pool.QueryRow(context.Background(), `SELECT COALESCE(revoke_reason,''),revoked_at IS NOT NULL
		FROM native_session_families WHERE id=$1`, grant.principal.familyID).Scan(&actual, &revoked); err != nil {
		t.Fatal(err)
	}
	if !revoked || actual != reason {
		t.Fatalf("revoke=%t reason=%s", revoked, actual)
	}
}

func TestNativeSessionHashOnlyPersistenceAndCurrentUser(t *testing.T) {
	pool, user, service, proof := nativeFixture(t)
	grant := nativeIssue(t, service, proof)
	nativeCounts(t, pool, 1, 1, 1)
	accessHash, refreshHash := grant.access.digest(), grant.refresh.digest()
	for _, item := range []struct {
		table string
		hash  []byte
	}{{"native_access_tokens", accessHash[:]}, {"native_refresh_tokens", refreshHash[:]}} {
		var persisted []byte
		if err := pool.QueryRow(context.Background(), `SELECT token_hash FROM `+item.table).Scan(&persisted); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(persisted, item.hash) || len(persisted) != 32 {
			t.Fatal("stored token is not its SHA256 hash")
		}
	}
	if grant.Principal().FamilyID() == uuid.Nil || grant.Principal().ClientInstanceID() == uuid.Nil ||
		grant.Principal().User().ID != user.ID || grant.Principal().TokenVersion() != proof.tokenVersion {
		t.Fatal("wrong principal")
	}
	if grant.FamilyExpiresAt().Sub(grant.AccessExpiresAt()) != nativeMaxFamilyTTL-nativeAccessTTL {
		t.Fatal("incorrect expiry difference")
	}
	if _, err := service.AuthenticateAccess(context.Background(), grant.refresh.wire()); err != ErrNativeUnauthorized {
		t.Fatal("refresh accepted as access")
	}
	if _, err := service.RotateRefresh(context.Background(), grant.access.wire()); err != ErrNativeUnauthorized {
		t.Fatal("access accepted as refresh")
	}
	unused := newNativeSecret()
	(*unused.value)[0] = 123
	if _, err := service.AuthenticateAccess(context.Background(), unused.wire()); err != ErrNativeUnauthorized {
		t.Fatal("unknown access accepted")
	}
	if _, err := service.RotateRefresh(context.Background(), unused.wire()); err != ErrNativeUnauthorized {
		t.Fatal("unknown refresh accepted")
	}
	if _, err := pool.Exec(context.Background(), `UPDATE users SET display_name='Updated',role='admin' WHERE id=$1`, user.ID); err != nil {
		t.Fatal(err)
	}
	principal, err := service.AuthenticateAccess(context.Background(), grant.access.wire())
	if err != nil || principal.User().DisplayName != "Updated" || !principal.User().IsAdmin() {
		t.Fatalf("fresh user: %v", err)
	}
	if err := service.RevokeFamily(context.Background(), principal); err != nil {
		t.Fatal(err)
	}
	if err := service.RevokeFamily(context.Background(), principal); err != nil {
		t.Fatal("revoke not idempotent")
	}
	nativeRevoked(t, pool, grant, "logout")
	if _, err := service.AuthenticateAccess(context.Background(), grant.access.wire()); err != ErrNativeUnauthorized {
		t.Fatal("revoked access accepted")
	}
	if _, err := service.RotateRefresh(context.Background(), grant.refresh.wire()); err != ErrNativeUnauthorized {
		t.Fatal("revoked refresh accepted")
	}
}

func TestNativeRefreshRotationAndCommittedReuseRevoke(t *testing.T) {
	pool, _, service, proof := nativeFixture(t)
	grant := nativeIssue(t, service, proof)
	if output, err := service.RotateRefresh(context.Background(), grant.refresh.wire()); err != ErrNativeRefreshWait || output != (IssuedNative{}) {
		t.Fatal("initial refresh did not throttle")
	}
	nativeCounts(t, pool, 1, 1, 1)
	nativeReady(t, pool, grant)
	next, err := service.RotateRefresh(context.Background(), grant.refresh.wire())
	if err != nil {
		t.Fatal(err)
	}
	if next.access.wire() == grant.access.wire() || next.refresh.wire() == grant.refresh.wire() || !next.FamilyExpiresAt().Equal(grant.FamilyExpiresAt()) {
		t.Fatal("rotation reused token or extended family")
	}
	nativeCounts(t, pool, 1, 2, 2)
	var consumed, live int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FILTER(WHERE consumed_at IS NOT NULL),
		count(*) FILTER(WHERE consumed_at IS NULL) FROM native_refresh_tokens`).Scan(&consumed, &live); err != nil {
		t.Fatal(err)
	}
	if consumed != 1 || live != 1 {
		t.Fatal("rotation did not leave one live refresh")
	}
	if _, err := service.AuthenticateAccess(context.Background(), grant.access.wire()); err != nil {
		t.Fatal("old unexpired access unexpectedly invalid")
	}
	if output, err := service.RotateRefresh(context.Background(), grant.refresh.wire()); err != ErrNativeUnauthorized || output != (IssuedNative{}) {
		t.Fatal("reuse not denied")
	}
	nativeRevoked(t, pool, grant, "refresh_reuse")
	if _, err := service.AuthenticateAccess(context.Background(), next.access.wire()); err != ErrNativeUnauthorized {
		t.Fatal("winner access survived reuse")
	}
	if _, err := service.RotateRefresh(context.Background(), next.refresh.wire()); err != ErrNativeUnauthorized {
		t.Fatal("successor survived reuse")
	}
}

func TestNativeRefreshExpiryClampingAndRotationLimit(t *testing.T) {
	t.Run("clamped access", func(t *testing.T) {
		pool, _, service, proof := nativeFixture(t)
		grant := nativeIssue(t, service, proof)
		var expiry time.Time
		if err := pool.QueryRow(context.Background(), `UPDATE native_session_families
			SET expires_at=clock_timestamp()+interval '10 seconds',refresh_after=created_at WHERE id=$1 RETURNING expires_at`, grant.principal.familyID).Scan(&expiry); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(context.Background(), `UPDATE native_refresh_tokens SET expires_at=$2 WHERE family_id=$1`, grant.principal.familyID, expiry); err != nil {
			t.Fatal(err)
		}
		next, err := service.RotateRefresh(context.Background(), grant.refresh.wire())
		if err != nil || !next.AccessExpiresAt().Equal(expiry) || !next.FamilyExpiresAt().Equal(expiry) {
			t.Fatalf("expiry clamp: %v", err)
		}
	})
	t.Run("sequence limit commits revoke", func(t *testing.T) {
		pool, _, service, proof := nativeFixture(t)
		grant := nativeIssue(t, service, proof)
		if _, err := pool.Exec(context.Background(), `UPDATE native_refresh_tokens SET sequence=$1`, nativeMaxSequence); err != nil {
			t.Fatal(err)
		}
		if _, err := service.RotateRefresh(context.Background(), grant.refresh.wire()); err != ErrNativeUnauthorized {
			t.Fatal(err)
		}
		nativeRevoked(t, pool, grant, "rotation_limit")
	})
	t.Run("malformed expiry relation", func(t *testing.T) {
		pool, _, service, proof := nativeFixture(t)
		grant := nativeIssue(t, service, proof)
		nativeReady(t, pool, grant)
		if _, err := pool.Exec(context.Background(), `UPDATE native_refresh_tokens SET expires_at=expires_at-interval '1 second'`); err != nil {
			t.Fatal(err)
		}
		if _, err := service.RotateRefresh(context.Background(), grant.refresh.wire()); err != ErrNativeUnauthorized {
			t.Fatal(err)
		}
		nativeCounts(t, pool, 1, 1, 1)
	})
}

func TestNativeExistingAccountRevocationsApply(t *testing.T) {
	for _, kind := range []string{"disable-enable", "logout-all", "password", "delete"} {
		t.Run(kind, func(t *testing.T) {
			pool, user, service, proof := nativeFixture(t)
			grant := nativeIssue(t, service, proof)
			var err error
			switch kind {
			case "disable-enable":
				err = SetDisabled(context.Background(), pool, user.ID, true)
				if err == nil {
					err = SetDisabled(context.Background(), pool, user.ID, false)
				}
			case "logout-all":
				err = RevokeSessions(context.Background(), pool, user.ID)
			case "password":
				_, err = ChangePassword(context.Background(), pool, user.ID, testPassword, "next-password-value")
			case "delete":
				_, err = pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, user.ID)
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := service.AuthenticateAccess(context.Background(), grant.access.wire()); err != ErrNativeUnauthorized {
				t.Fatal("account revoke did not block access")
			}
			if _, err := service.RotateRefresh(context.Background(), grant.refresh.wire()); err != ErrNativeUnauthorized {
				t.Fatal("account revoke did not block refresh")
			}
			if _, err := service.IssueVerified(context.Background(), proof, uuid.New()); err != ErrNativeUnauthorized {
				t.Fatal("old proof accepted")
			}
			if kind == "delete" {
				nativeCounts(t, pool, 0, 0, 0)
			} else {
				nativeRevoked(t, pool, grant, "version_mismatch")
			}
		})
	}
}

func TestNativeConcurrentRefreshAndFamilyLimit(t *testing.T) {
	t.Run("same refresh revokes the winner", func(t *testing.T) {
		pool, _, service, proof := nativeFixture(t)
		grant := nativeIssue(t, service, proof)
		nativeReady(t, pool, grant)
		type reply struct {
			issued IssuedNative
			err    error
		}
		replies := make(chan reply, 2)
		start := make(chan struct{})
		for range 2 {
			go func() {
				<-start
				issued, err := service.RotateRefresh(context.Background(), grant.refresh.wire())
				replies <- reply{issued, err}
			}()
		}
		close(start)
		success := 0
		for range 2 {
			reply := <-replies
			if reply.err == nil {
				success++
			} else if reply.err != ErrNativeUnauthorized {
				t.Fatal(reply.err)
			}
		}
		if success != 1 {
			t.Fatalf("success count=%d", success)
		}
		nativeRevoked(t, pool, grant, "refresh_reuse")
	})
	t.Run("user lock enforces cap", func(t *testing.T) {
		pool, _, service, proof := nativeFixture(t)
		for range nativeMaxFamilies - 1 {
			nativeIssue(t, service, proof)
		}
		replies := make(chan error, 2)
		start := make(chan struct{})
		for range 2 {
			go func() {
				<-start
				_, err := service.IssueVerified(context.Background(), proof, uuid.New())
				replies <- err
			}()
		}
		close(start)
		success := 0
		for range 2 {
			err := <-replies
			if err == nil {
				success++
			} else if err != ErrNativeLimit {
				t.Fatal(err)
			}
		}
		if success != 1 {
			t.Fatal("concurrent issuance exceeded cap")
		}
		nativeCounts(t, pool, nativeMaxFamilies, nativeMaxFamilies, nativeMaxFamilies)
	})
}
