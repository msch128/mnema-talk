//go:build integration

package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func TestNativeTransactionRejectsMissingScopeWithoutMutation(t *testing.T) {
	_, _, service, proof := nativeFixture(t)
	grant := nativeIssue(t, service, proof)
	principal := grant.Principal()
	if err := service.WithAuthorizedTransaction(context.Background(), principal, nil); !errors.Is(err, ErrNativeUnauthorized) {
		t.Fatal("missing callback acquired transaction authority")
	}
	for _, boundary := range []string{"empty", "missing-user", "missing-family", "foreign-instance", "expired-access", "expired-family"} {
		t.Run(boundary, func(t *testing.T) {
			captured := principal
			switch boundary {
			case "empty":
				captured = NativePrincipal{}
			case "missing-user":
				captured.user.ID = uuid.New()
			case "missing-family":
				captured.familyID = uuid.New()
			case "foreign-instance":
				captured.instanceID = uuid.New()
			case "expired-access":
				captured.accessExpiresAt = time.Now().Add(-time.Hour)
			case "expired-family":
				captured.familyExpiresAt = time.Now().Add(-time.Hour)
			}
			called := false
			err := service.WithAuthorizedTransaction(context.Background(), captured, func(context.Context, pgx.Tx, User) error {
				called = true
				return nil
			})
			if !errors.Is(err, ErrNativeUnauthorized) || called {
				t.Fatal("missing or expired captured scope reached a mutation")
			}
		})
	}
	if _, err := service.RevalidateNativeLease(context.Background(), principal); err != nil {
		t.Fatal("scope rejection revoked the valid original family")
	}
}

func TestNativeTransactionDatabaseFailuresNeverReachMutation(t *testing.T) {
	pool, _, service, proof := nativeFixture(t)
	grant := nativeIssue(t, service, proof)
	for _, query := range []string{"FROM users WHERE id=$1 FOR UPDATE", "FROM native_session_families WHERE id=$1 AND user_id=$2 FOR UPDATE", "SELECT clock_timestamp()"} {
		t.Run(query, func(t *testing.T) {
			fault := nativeFaultService(t, pool, &authQueryHook{match: query, cancel: true})
			called := false
			err := fault.WithAuthorizedTransaction(context.Background(), grant.Principal(), func(context.Context, pgx.Tx, User) error {
				called = true
				return nil
			})
			if !errors.Is(err, context.Canceled) || called {
				t.Fatal("failed database authorization reached a mutation or lost its cancellation cause")
			}
		})
	}
	if _, err := service.RevalidateNativeLease(context.Background(), grant.Principal()); err != nil {
		t.Fatal("failed transaction invalidated the persistent family")
	}
}

func TestNativeTransactionCurrentAccountAndRollback(t *testing.T) {
	pool, user, service, proof := nativeFixture(t)
	grant := nativeIssue(t, service, proof)
	principal, err := service.AuthenticateAccess(context.Background(), grant.access.wire())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(context.Background(), `UPDATE users SET display_name='Current account',role='admin' WHERE id=$1`, user.ID); err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("fixture rollback")
	err = service.WithAuthorizedTransaction(context.Background(), principal, func(ctx context.Context, tx pgx.Tx, current User) error {
		if current.DisplayName != "Current account" || !current.IsAdmin() {
			t.Fatal("stale account authorized")
		}
		_, e := tx.Exec(ctx, `UPDATE users SET display_name='Must roll back' WHERE id=$1`, current.ID)
		if e != nil {
			return e
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("callback failure: %v", err)
	}
	var name string
	if err = pool.QueryRow(context.Background(), `SELECT display_name FROM users WHERE id=$1`, user.ID).Scan(&name); err != nil || name != "Current account" {
		t.Fatalf("rollback: %q %v", name, err)
	}
	if err = service.WithAuthorizedTransaction(context.Background(), principal, func(ctx context.Context, tx pgx.Tx, current User) error {
		_, e := tx.Exec(ctx, `UPDATE users SET display_name='Committed' WHERE id=$1`, current.ID)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(context.Background(), `SELECT display_name FROM users WHERE id=$1`, user.ID).Scan(&name); err != nil || name != "Committed" {
		t.Fatalf("commit: %q %v", name, err)
	}
}

func TestNativeTransactionRejectsRevokedCapturedPrincipal(t *testing.T) {
	_, _, service, proof := nativeFixture(t)
	grant := nativeIssue(t, service, proof)
	principal, err := service.AuthenticateAccess(context.Background(), grant.access.wire())
	if err != nil {
		t.Fatal(err)
	}
	if err = service.RevokeFamily(context.Background(), principal); err != nil {
		t.Fatal(err)
	}
	called := false
	err = service.WithAuthorizedTransaction(context.Background(), principal, func(context.Context, pgx.Tx, User) error { called = true; return nil })
	if !errors.Is(err, ErrNativeUnauthorized) || called {
		t.Fatalf("revoked action: %v called=%t", err, called)
	}
}

func TestNativeTransactionExpiresDuringAction(t *testing.T) {
	pool, user, service, proof := nativeFixture(t)
	grant := nativeIssue(t, service, proof)
	hash := grant.access.digest()
	if _, err := pool.Exec(context.Background(), `UPDATE native_access_tokens SET expires_at=clock_timestamp()+interval '1 second' WHERE token_hash=$1`, hash[:]); err != nil {
		t.Fatal(err)
	}
	principal, err := service.AuthenticateAccess(context.Background(), grant.access.wire())
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	err = service.WithAuthorizedTransaction(context.Background(), principal, func(ctx context.Context, tx pgx.Tx, current User) error {
		if _, e := tx.Exec(ctx, `UPDATE users SET display_name='Expired mutation' WHERE id=$1`, current.ID); e != nil {
			return e
		}
		_, e := tx.Exec(ctx, `SELECT pg_sleep(1.2)`)
		return e
	})
	if !errors.Is(err, ErrNativeUnauthorized) || time.Since(started) < time.Second {
		t.Fatalf("expired action: %v", err)
	}
	var name string
	if err = pool.QueryRow(context.Background(), `SELECT display_name FROM users WHERE id=$1`, user.ID).Scan(&name); err != nil || name == "Expired mutation" {
		t.Fatalf("expired mutation committed: %q %v", name, err)
	}
}

func TestNativeTransactionRechecksAfterAccountLock(t *testing.T) {
	pool, user, service, proof := nativeFixture(t)
	grant := nativeIssue(t, service, proof)
	principal, err := service.AuthenticateAccess(context.Background(), grant.access.wire())
	if err != nil {
		t.Fatal(err)
	}
	lock, err := pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Rollback(context.Background())
	if _, err = lock.Exec(context.Background(), `UPDATE users SET disabled_at=clock_timestamp() WHERE id=$1`, user.ID); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	done := make(chan error, 1)
	called := false
	go func() {
		close(started)
		done <- service.WithAuthorizedTransaction(context.Background(), principal, func(context.Context, pgx.Tx, User) error { called = true; return nil })
	}()
	<-started
	select {
	case err := <-done:
		t.Fatalf("mutation escaped held lock: %v", err)
	case <-time.After(75 * time.Millisecond):
	}
	if err = lock.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-done:
		if !errors.Is(err, ErrNativeUnauthorized) || called {
			t.Fatalf("disabled action: %v called=%t", err, called)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("action did not resume")
	}
}
