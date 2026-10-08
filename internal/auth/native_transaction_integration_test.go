//go:build integration

package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

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
