//go:build integration

package auth

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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

func nativeFaultService(t *testing.T, pool *db.Pool, hook *authQueryHook) *NativeSessions {
	t.Helper()
	service, err := NewNativeSessions(authHookPool(t, pool, hook), NativePolicy{SessionLifetime: nativeMaxFamilyTTL, FamilyLifetime: nativeMaxFamilyTTL})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestNativeCredentialProofAndRefreshRevocationRaces(t *testing.T) {
	for _, rotate := range []bool{false, true} {
		for _, action := range []string{"revoke", "disable", "password"} {
			name := action
			if rotate {
				name += "-refresh"
			} else {
				name += "-issue"
			}
			t.Run(name, func(t *testing.T) {
				pool, user, service, proof := nativeFixture(t)
				var grant IssuedNative
				if rotate {
					grant = nativeIssue(t, service, proof)
					nativeReady(t, pool, grant)
				}
				hook := &authQueryHook{match: "FROM users WHERE id=$1 FOR UPDATE", before: func() {
					var err error
					switch action {
					case "revoke":
						err = RevokeSessions(context.Background(), pool, user.ID)
					case "disable":
						err = SetDisabled(context.Background(), pool, user.ID, true)
					case "password":
						_, err = ChangePassword(context.Background(), pool, user.ID, testPassword, "next-password-value")
					}
					if err != nil {
						t.Fatal(err)
					}
				}}
				fault := nativeFaultService(t, pool, hook)
				var output IssuedNative
				var err error
				if rotate {
					output, err = fault.RotateRefresh(context.Background(), grant.refresh.wire())
				} else {
					output, err = fault.IssueVerified(context.Background(), proof, uuid.New())
				}
				if err != ErrNativeUnauthorized || output != (IssuedNative{}) {
					t.Fatalf("stale proof/refresh: %v", err)
				}
				if !rotate {
					nativeCounts(t, pool, 0, 0, 0)
				}
			})
		}
	}
	t.Run("revoke waits for issuance lock", func(t *testing.T) {
		pool, user, _, proof := nativeFixture(t)
		started, done := make(chan struct{}), make(chan error, 1)
		revokePool := authHookPool(t, pool, &authQueryHook{match: "UPDATE users SET token_version", before: func() { close(started) }})
		fault := nativeFaultService(t, pool, &authQueryHook{match: "SELECT clock_timestamp()", before: func() {
			go func() { done <- RevokeSessions(context.Background(), revokePool, user.ID) }()
			<-started
		}})
		grant := nativeIssue(t, fault, proof)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if _, err := fault.AuthenticateAccess(context.Background(), grant.access.wire()); err != ErrNativeUnauthorized {
			t.Fatal("post-commit revoke did not invalidate grant")
		}
	})
}

func TestNativeWriteCancellationRollsBackWholeGrant(t *testing.T) {
	for _, query := range []string{"FROM users WHERE id=$1 FOR UPDATE", "SELECT clock_timestamp()", "SELECT count(*) FROM native_session_families",
		"INSERT INTO native_session_families", "INSERT INTO native_access_tokens", "INSERT INTO native_refresh_tokens"} {
		t.Run(query, func(t *testing.T) {
			pool, _, _, proof := nativeFixture(t)
			fault := nativeFaultService(t, pool, &authQueryHook{match: query, cancel: true})
			output, err := fault.IssueVerified(context.Background(), proof, uuid.New())
			if !errors.Is(err, context.Canceled) || output != (IssuedNative{}) {
				t.Fatalf("cancellation: %v", err)
			}
			nativeCounts(t, pool, 0, 0, 0)
		})
	}
	for _, query := range []string{"SELECT r.family_id,f.user_id", "FROM users WHERE id=$1 FOR UPDATE", "FROM native_session_families WHERE id=$1",
		"FROM native_refresh_tokens WHERE token_hash=$1", "SELECT clock_timestamp()", "UPDATE native_refresh_tokens SET consumed_at",
		"INSERT INTO native_access_tokens", "INSERT INTO native_refresh_tokens", "UPDATE native_session_families SET refresh_after"} {
		t.Run("refresh-"+query, func(t *testing.T) {
			pool, _, service, proof := nativeFixture(t)
			grant := nativeIssue(t, service, proof)
			nativeReady(t, pool, grant)
			fault := nativeFaultService(t, pool, &authQueryHook{match: query, cancel: true})
			output, err := fault.RotateRefresh(context.Background(), grant.refresh.wire())
			if !errors.Is(err, context.Canceled) || output != (IssuedNative{}) {
				t.Fatalf("refresh cancellation: %v", err)
			}
			nativeCounts(t, pool, 1, 1, 1)
			if _, err := service.RotateRefresh(context.Background(), grant.refresh.wire()); err != nil {
				t.Fatal("rollback consumed old refresh")
			}
		})
	}
}

type nativeLostCommitTx struct{ pgx.Tx }

func (tx nativeLostCommitTx) Commit(ctx context.Context) error {
	if err := tx.Tx.Commit(ctx); err != nil {
		return err
	}
	return io.ErrUnexpectedEOF
}

func TestNativeCommitFailuresNeverReturnSecrets(t *testing.T) {
	t.Run("commit canceled", func(t *testing.T) {
		pool, _, _, proof := nativeFixture(t)
		fault := nativeFaultService(t, pool, &authQueryHook{match: "commit", cancel: true})
		output, err := fault.IssueVerified(context.Background(), proof, uuid.New())
		if err == nil || output != (IssuedNative{}) {
			t.Fatal("failed commit returned secrets")
		}
		nativeCounts(t, pool, 0, 0, 0)
	})
	t.Run("committed refresh acknowledgement lost", func(t *testing.T) {
		pool, _, service, proof := nativeFixture(t)
		grant := nativeIssue(t, service, proof)
		nativeReady(t, pool, grant)
		fault, err := NewNativeSessions(pool, service.policy)
		if err != nil {
			t.Fatal(err)
		}
		fault.begin = func(ctx context.Context) (pgx.Tx, error) {
			tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
			return nativeLostCommitTx{Tx: tx}, err
		}
		output, err := fault.RotateRefresh(context.Background(), grant.refresh.wire())
		if !errors.Is(err, io.ErrUnexpectedEOF) || output != (IssuedNative{}) {
			t.Fatal("lost ack returned secrets")
		}
		nativeCounts(t, pool, 1, 2, 2)
		if _, err := service.RotateRefresh(context.Background(), grant.refresh.wire()); err != ErrNativeUnauthorized {
			t.Fatal("lost-ack retry bypassed replay")
		}
		nativeRevoked(t, pool, grant, "refresh_reuse")
	})
}

func TestNativeClockAfterLockWaitRejectsExpiredRefresh(t *testing.T) {
	pool, user, service, proof := nativeFixture(t)
	grant := nativeIssue(t, service, proof)
	nativeReady(t, pool, grant)
	locker, err := pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer locker.Rollback(context.Background())
	if _, err := locker.Exec(context.Background(), `SELECT id FROM users WHERE id=$1 FOR UPDATE`, user.ID); err != nil {
		t.Fatal(err)
	}
	waiting := make(chan struct{})
	fault := nativeFaultService(t, pool, &authQueryHook{match: "FROM users WHERE id=$1 FOR UPDATE", before: func() { close(waiting) }})
	done := make(chan error, 1)
	go func() { _, err := fault.RotateRefresh(context.Background(), grant.refresh.wire()); done <- err }()
	<-waiting
	// Expiry occurs after the refresh transaction began, but before it obtains
	// the user lock. A transaction-start now() would incorrectly admit it.
	var expiry time.Time
	if err := locker.QueryRow(context.Background(), `SELECT clock_timestamp()`).Scan(&expiry); err != nil {
		t.Fatal(err)
	}
	if _, err := locker.Exec(context.Background(), `UPDATE native_session_families SET created_at=$2,
		expires_at=$3,refresh_after=$2 WHERE id=$1`, grant.principal.familyID, expiry.Add(-time.Hour), expiry); err != nil {
		t.Fatal(err)
	}
	if _, err := locker.Exec(context.Background(), `UPDATE native_refresh_tokens SET expires_at=$2 WHERE family_id=$1`, grant.principal.familyID, expiry); err != nil {
		t.Fatal(err)
	}
	if err := locker.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != ErrNativeUnauthorized {
		t.Fatalf("expired after lock wait: %v", err)
	}
}

func rejectNativeWrite(t *testing.T, pool *db.Pool, table, operation string) {
	t.Helper()
	name := "native_reject_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := pool.Exec(context.Background(), "CREATE FUNCTION "+name+`() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic private database detail'; END $$`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), "CREATE TRIGGER "+name+" BEFORE "+operation+" ON "+table+" FOR EACH ROW EXECUTE FUNCTION "+name+"()"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), "DROP TRIGGER "+name+" ON "+table); err != nil {
			t.Error(err)
		}
		if _, err := pool.Exec(context.Background(), "DROP FUNCTION "+name+"()"); err != nil {
			t.Error(err)
		}
	})
}

func TestNativeRejectedWritesAndReplayRevokeFailures(t *testing.T) {
	for _, table := range []string{"native_session_families", "native_access_tokens", "native_refresh_tokens"} {
		t.Run(table, func(t *testing.T) {
			pool, _, service, proof := nativeFixture(t)
			rejectNativeWrite(t, pool, table, "INSERT")
			output, err := service.IssueVerified(context.Background(), proof, uuid.New())
			if err == nil || output != (IssuedNative{}) || strings.Contains(err.Error(), "synthetic private database detail") {
				t.Fatal("rejected insert leaked or issued grant")
			}
			nativeCounts(t, pool, 0, 0, 0)
		})
	}
	t.Run("security revoke write fails", func(t *testing.T) {
		pool, _, service, proof := nativeFixture(t)
		grant := nativeIssue(t, service, proof)
		nativeReady(t, pool, grant)
		if _, err := service.RotateRefresh(context.Background(), grant.refresh.wire()); err != nil {
			t.Fatal(err)
		}
		rejectNativeWrite(t, pool, "native_session_families", "UPDATE")
		_, err := service.RotateRefresh(context.Background(), grant.refresh.wire())
		var storeErr nativeStoreError
		if !errors.As(err, &storeErr) {
			t.Fatal("failed revoke claimed auth-only outcome")
		}
		var revoked bool
		if err := pool.QueryRow(context.Background(), `SELECT revoked_at IS NOT NULL FROM native_session_families`).Scan(&revoked); err != nil || revoked {
			t.Fatal("failed revoke unexpectedly committed")
		}
	})
}

func TestNativeCleanupPreservesReplayTombstonesAndBounds(t *testing.T) {
	pool, _, service, proof := nativeFixture(t)
	grant := nativeIssue(t, service, proof)
	nativeReady(t, pool, grant)
	next, err := service.RotateRefresh(context.Background(), grant.refresh.wire())
	if err != nil {
		t.Fatal(err)
	}
	if n, err := service.Cleanup(context.Background(), 1); err != nil || n != 0 {
		t.Fatalf("active cleanup=%d err=%v", n, err)
	}
	nativeCounts(t, pool, 1, 2, 2)
	// Expire one access row, while retaining the live family's consumed hash.
	hash := grant.access.digest()
	if _, err := pool.Exec(context.Background(), `WITH c AS MATERIALIZED(SELECT clock_timestamp() n)
		UPDATE native_access_tokens SET created_at=c.n-interval '6 minutes',expires_at=c.n-interval '1 minute'
		FROM c WHERE token_hash=$1`, hash[:]); err != nil {
		t.Fatal(err)
	}
	if _, err := service.AuthenticateAccess(context.Background(), grant.access.wire()); err != ErrNativeUnauthorized {
		t.Fatal("expired access accepted")
	}
	if n, err := service.Cleanup(context.Background(), 1); err != nil || n != 1 {
		t.Fatalf("access cleanup=%d err=%v", n, err)
	}
	nativeCounts(t, pool, 1, 1, 2)
	if _, err := service.RotateRefresh(context.Background(), grant.refresh.wire()); err != ErrNativeUnauthorized {
		t.Fatal("cleanup erased replay detection")
	}
	nativeRevoked(t, pool, next, "refresh_reuse")
	if n, err := service.Cleanup(context.Background(), 1); err != nil || n != 1 {
		t.Fatalf("family cleanup=%d err=%v", n, err)
	}
	nativeCounts(t, pool, 0, 0, 0)
	// Multiple eligible families still respect the requested batch.
	for range 3 {
		nativeIssue(t, service, proof)
	}
	if err := RevokeSessions(context.Background(), pool, proof.userID); err != nil {
		t.Fatal(err)
	}
	if n, err := service.Cleanup(context.Background(), 1); err != nil || n != 1 {
		t.Fatal("cleanup exceeded or missed batch")
	}
	nativeCounts(t, pool, 2, 2, 2)
}

func TestNativeEntropyBeginAndDatabaseAuthErrors(t *testing.T) {
	pool, _, service, proof := nativeFixture(t)
	service.random = nativeBrokenReader{}
	if output, err := service.IssueVerified(context.Background(), proof, uuid.New()); err != errNativeEntropy || output != (IssuedNative{}) {
		t.Fatal("entropy failure issued")
	}
	service.random = bytes.NewReader(append(bytes.Repeat([]byte{1}, 32), bytes.Repeat([]byte{2}, 32)...))
	if _, err := service.IssueVerified(context.Background(), proof, uuid.New()); err != errNativeEntropy {
		t.Fatal("UUID entropy failure not handled")
	}
	nativeCounts(t, pool, 0, 0, 0)
	base, err := NewNativeSessions(pool, service.policy)
	if err != nil {
		t.Fatal(err)
	}
	grant := nativeIssue(t, base, proof)
	fault := nativeFaultService(t, pool, &authQueryHook{match: "FROM native_access_tokens a JOIN native_session_families", cancel: true})
	if principal, err := fault.AuthenticateAccess(context.Background(), grant.access.wire()); !errors.Is(err, context.Canceled) || principal != (NativePrincipal{}) {
		t.Fatal("database auth error lost")
	}
	fault.begin = func(context.Context) (pgx.Tx, error) { return nil, errors.New("synthetic begin detail") }
	if output, err := fault.IssueVerified(context.Background(), proof, uuid.New()); err == nil || output != (IssuedNative{}) || strings.Contains(err.Error(), "synthetic begin detail") {
		t.Fatal("begin failure unsafe")
	}
	nativeCounts(t, pool, 1, 1, 1)
}

func TestNativeDisappearingRefreshLocatorsFailClosed(t *testing.T) {
	for _, boundary := range []string{"user", "family", "refresh"} {
		t.Run(boundary, func(t *testing.T) {
			pool, user, service, proof := nativeFixture(t)
			grant := nativeIssue(t, service, proof)
			nativeReady(t, pool, grant)
			query := "FROM users WHERE id=$1 FOR UPDATE"
			if boundary == "family" {
				query = "FROM native_session_families WHERE id=$1"
			}
			if boundary == "refresh" {
				query = "FROM native_refresh_tokens WHERE token_hash=$1"
			}
			fault := nativeFaultService(t, pool, &authQueryHook{match: query, before: func() {
				var err error
				switch boundary {
				case "user":
					_, err = pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, user.ID)
				case "family":
					_, err = pool.Exec(context.Background(), `DELETE FROM native_session_families WHERE id=$1`, grant.principal.familyID)
				case "refresh":
					_, err = pool.Exec(context.Background(), `DELETE FROM native_refresh_tokens WHERE family_id=$1`, grant.principal.familyID)
				}
				if err != nil {
					t.Fatal(err)
				}
			}})
			output, err := fault.RotateRefresh(context.Background(), grant.refresh.wire())
			if err != ErrNativeUnauthorized || output != (IssuedNative{}) {
				t.Fatalf("disappearing locator: %v", err)
			}
		})
	}
}

func TestNativeRefreshEntropyOrSuppressedConsumePreservesOldState(t *testing.T) {
	t.Run("entropy unavailable during rotation", func(t *testing.T) {
		pool, _, service, proof := nativeFixture(t)
		grant := nativeIssue(t, service, proof)
		nativeReady(t, pool, grant)
		service.random = nativeBrokenReader{}
		if output, err := service.RotateRefresh(context.Background(), grant.refresh.wire()); err != errNativeEntropy || output != (IssuedNative{}) {
			t.Fatal("rotation entropy failure unsafe")
		}
		nativeCounts(t, pool, 1, 1, 1)
	})
	t.Run("trigger suppresses consume", func(t *testing.T) {
		pool, _, service, proof := nativeFixture(t)
		grant := nativeIssue(t, service, proof)
		nativeReady(t, pool, grant)
		if _, err := pool.Exec(context.Background(), `CREATE FUNCTION native_skip_consume() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NULL; END $$`); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(context.Background(), `CREATE TRIGGER native_skip_consume BEFORE UPDATE ON native_refresh_tokens FOR EACH ROW EXECUTE FUNCTION native_skip_consume()`); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if _, err := pool.Exec(context.Background(), `DROP TRIGGER native_skip_consume ON native_refresh_tokens`); err != nil {
				t.Error(err)
			}
			if _, err := pool.Exec(context.Background(), `DROP FUNCTION native_skip_consume()`); err != nil {
				t.Error(err)
			}
		})
		if output, err := service.RotateRefresh(context.Background(), grant.refresh.wire()); err != ErrNativeUnauthorized || output != (IssuedNative{}) {
			t.Fatal("suppressed consumption issued secrets")
		}
		nativeCounts(t, pool, 1, 1, 1)
	})
}

func TestNativeRevokeOwnershipExpiryAndDatabaseErrors(t *testing.T) {
	for _, state := range []string{"missing-user", "missing-family", "different-user", "other-existing-user", "wrong-instance", "expired-principal", "version-bump", "disabled"} {
		t.Run(state, func(t *testing.T) {
			pool, user, service, proof := nativeFixture(t)
			grant := nativeIssue(t, service, proof)
			principal := grant.principal
			var err error
			switch state {
			case "missing-user":
				_, err = pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, user.ID)
			case "missing-family":
				_, err = pool.Exec(context.Background(), `DELETE FROM native_session_families WHERE id=$1`, principal.familyID)
			case "different-user":
				principal.user.ID = uuid.New()
			case "other-existing-user":
				err = pool.QueryRow(context.Background(), `SELECT id FROM users WHERE role='admin'`).Scan(&principal.user.ID)
			case "wrong-instance":
				principal.instanceID = uuid.New()
			case "expired-principal":
				principal.accessExpiresAt = time.Unix(1, 0)
			case "version-bump":
				err = RevokeSessions(context.Background(), pool, user.ID)
			case "disabled":
				err = SetDisabled(context.Background(), pool, user.ID, true)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := service.RevokeFamily(context.Background(), principal); err != ErrNativeUnauthorized {
				t.Fatalf("invalid principal: %v", err)
			}
		})
	}
	for _, query := range []string{"FROM users WHERE id=$1 FOR UPDATE", "FROM native_session_families WHERE id=$1", "SELECT clock_timestamp()", "UPDATE native_session_families SET revoked_at"} {
		t.Run(query, func(t *testing.T) {
			pool, _, service, proof := nativeFixture(t)
			grant := nativeIssue(t, service, proof)
			fault := nativeFaultService(t, pool, &authQueryHook{match: query, cancel: true})
			if err := fault.RevokeFamily(context.Background(), grant.principal); !errors.Is(err, context.Canceled) {
				t.Fatal("revoke database failure lost")
			}
			if _, err := service.AuthenticateAccess(context.Background(), grant.access.wire()); err != nil {
				t.Fatal("failed revoke changed grant")
			}
		})
	}
}

func TestNativeCleanupCancellationRollsBackDeletions(t *testing.T) {
	for _, query := range []string{"SELECT f.id,f.user_id FROM native_session_families", "FROM users WHERE id=$1 FOR UPDATE SKIP LOCKED",
		"FROM native_session_families WHERE id=$1 AND user_id=$2 FOR UPDATE SKIP LOCKED", "SELECT clock_timestamp()", "DELETE FROM native_session_families", "DELETE FROM native_access_tokens"} {
		t.Run(query, func(t *testing.T) {
			pool, _, service, proof := nativeFixture(t)
			grant := nativeIssue(t, service, proof)
			if err := service.RevokeFamily(context.Background(), grant.principal); err != nil {
				t.Fatal(err)
			}
			// On the access-delete fault, the preceding family deletion must
			// also roll back: partial cleanup results are never returned.
			fault := nativeFaultService(t, pool, &authQueryHook{match: query, cancel: true})
			n, err := fault.Cleanup(context.Background(), 2)
			if !errors.Is(err, context.Canceled) || n != 0 {
				t.Fatalf("cleanup failure=%d %v", n, err)
			}
			nativeCounts(t, pool, 1, 1, 1)
		})
	}
}

func TestNativeCleanupSkipsLockedOrDisappearedFamilies(t *testing.T) {
	for _, boundary := range []string{"user-locked", "family-locked", "user-deleted", "family-deleted", "eligibility-changed"} {
		t.Run(boundary, func(t *testing.T) {
			pool, user, service, proof := nativeFixture(t)
			grant := nativeIssue(t, service, proof)
			if err := service.RevokeFamily(context.Background(), grant.principal); err != nil {
				t.Fatal(err)
			}
			if boundary == "user-locked" || boundary == "family-locked" {
				locker, err := pool.Begin(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				defer locker.Rollback(context.Background())
				query, id := `SELECT id FROM users WHERE id=$1 FOR UPDATE`, user.ID
				if boundary == "family-locked" {
					query, id = `SELECT id FROM native_session_families WHERE id=$1 FOR UPDATE`, grant.principal.familyID
				}
				if _, err := locker.Exec(context.Background(), query, id); err != nil {
					t.Fatal(err)
				}
				if n, err := service.Cleanup(context.Background(), 1); err != nil || n != 0 {
					t.Fatalf("locked cleanup=%d %v", n, err)
				}
				nativeCounts(t, pool, 1, 1, 1)
				if err := locker.Rollback(context.Background()); err != nil {
					t.Fatal(err)
				}
				if n, err := service.Cleanup(context.Background(), 1); err != nil || n != 1 {
					t.Fatal("released cleanup failed")
				}
				return
			}
			match := "FROM users WHERE id=$1 FOR UPDATE SKIP LOCKED"
			if boundary == "family-deleted" {
				match = "FROM native_session_families WHERE id=$1 AND user_id=$2 FOR UPDATE SKIP LOCKED"
			}
			fault := nativeFaultService(t, pool, &authQueryHook{match: match, before: func() {
				var err error
				switch boundary {
				case "user-deleted":
					_, err = pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, user.ID)
				case "family-deleted":
					_, err = pool.Exec(context.Background(), `DELETE FROM native_session_families WHERE id=$1`, grant.principal.familyID)
				case "eligibility-changed":
					_, err = pool.Exec(context.Background(), `UPDATE native_session_families SET revoked_at=NULL,revoke_reason=NULL WHERE id=$1`, grant.principal.familyID)
				}
				if err != nil {
					t.Fatal(err)
				}
			}})
			if n, err := fault.Cleanup(context.Background(), 1); err != nil || n != 0 {
				t.Fatalf("stale cleanup locator=%d %v", n, err)
			}
			if boundary == "eligibility-changed" {
				nativeCounts(t, pool, 1, 1, 1)
			} else {
				nativeCounts(t, pool, 0, 0, 0)
			}
		})
	}
}

func TestNativeSchemaRejectsBrokenSecurityConstraints(t *testing.T) {
	pool, _, service, proof := nativeFixture(t)
	grant := nativeIssue(t, service, proof)
	for _, query := range []string{
		`UPDATE native_session_families SET expires_at=created_at+interval '721 hours'`,
		`UPDATE native_session_families SET issued_token_version=-1`,
		`UPDATE native_session_families SET revoked_at=clock_timestamp()`,
		`UPDATE native_session_families SET revoke_reason='logout'`,
		`UPDATE native_access_tokens SET token_hash='x'::bytea`,
		`UPDATE native_access_tokens SET expires_at=created_at+interval '6 minutes'`,
		`UPDATE native_refresh_tokens SET sequence=65536`,
		`UPDATE native_refresh_tokens SET expires_at=created_at`,
		`UPDATE native_refresh_tokens SET consumed_at=created_at-interval '1 second'`,
	} {
		if _, err := pool.Exec(context.Background(), query); err == nil {
			t.Fatal("invalid schema state accepted")
		}
	}
	nativeReady(t, pool, grant)
	if _, err := service.RotateRefresh(context.Background(), grant.refresh.wire()); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), `UPDATE native_refresh_tokens SET consumed_at=NULL`); err == nil {
		t.Fatal("multiple live refresh tokens accepted")
	}
	nativeCounts(t, pool, 1, 2, 2)
}
