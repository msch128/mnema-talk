//go:build integration

package db

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// flushDelay is how long every WAL flush of slowFlushPool's one session takes.
const flushDelay = 100 * time.Millisecond

// slowFlushPool is a scratch pool on one session whose WAL flushes are held
// back by flushDelay (commit_delay, with no other transactions needed): it
// tells a commit that flushes from one that does not. Setting commit_delay
// needs a superuser, as the test databases have; elsewhere the test skips.
func slowFlushPool(t *testing.T) *Pool {
	t.Helper()
	p := scratchPoolWith(t, Options{MaxConns: 1, StatementTimeout: 10 * time.Second})
	ctx := context.Background()
	if _, err := p.Exec(ctx, `SET commit_delay = 100000`); err != nil {
		t.Skipf("cannot hold back WAL flushes here: %v", err)
	}
	if _, err := p.Exec(ctx, `SET commit_siblings = 0`); err != nil {
		t.Fatal(err)
	}
	var fsync string
	if err := p.QueryRow(ctx, `SHOW fsync`).Scan(&fsync); err != nil || fsync != "on" {
		t.Skipf("fsync = %q (%v): commit_delay applies only with fsync on", fsync, err)
	}
	return p
}

func timed(t *testing.T, what string, f func() error) time.Duration {
	t.Helper()
	start := time.Now()
	if err := f(); err != nil {
		t.Fatalf("%s: %v", what, err)
	}
	return time.Since(start)
}

func TestAsyncCommitSkipsTheFlushAndAwaitDurableDoesIt(t *testing.T) {
	p := slowFlushPool(t)
	ctx := context.Background()
	if _, err := p.Exec(ctx, `CREATE TABLE durable_probe (n int)`); err != nil {
		t.Fatal(err)
	}
	// A plain commit waits for the held-back flush: the probe works here.
	if d := timed(t, "synchronous insert", func() error {
		_, err := p.Exec(ctx, `INSERT INTO durable_probe VALUES (0)`)
		return err
	}); d < flushDelay {
		t.Skipf("a synchronous commit took %v, less than the flush delay %v", d, flushDelay)
	}

	tx, err := p.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := AsyncCommit(ctx, tx); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO durable_probe VALUES (1)`); err != nil {
		t.Fatal(err)
	}
	if d := timed(t, "asynchronous commit", func() error { return tx.Commit(ctx) }); d >= flushDelay {
		t.Fatalf("the asynchronous commit took %v: it waited for the WAL flush (%v)", d, flushDelay)
	}
	if d := timed(t, "AwaitDurable", func() error { return AwaitDurable(ctx, p) }); d < flushDelay {
		t.Fatalf("AwaitDurable returned after %v, before a WAL flush (%v)", d, flushDelay)
	}
	var mode string
	if err := p.QueryRow(ctx, `SHOW synchronous_commit`).Scan(&mode); err != nil || mode != "on" {
		t.Fatalf("synchronous_commit after the transaction = %q (%v), want on: the setting must end with it", mode, err)
	}
}

func TestAwaitDurableIsNotCutShortByTheCallersCancellation(t *testing.T) {
	p := slowFlushPool(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := AwaitDurable(ctx, p); err != nil {
		t.Fatalf("AwaitDurable with a cancelled context: %v", err)
	}
	// The pool's one session still works.
	if err := pgx.BeginFunc(context.Background(), p, func(pgx.Tx) error { return nil }); err != nil {
		t.Fatal(err)
	}
}

// The application's role need not be a superuser: the logical message is
// open to every role.
func TestAwaitDurableNeedsNoPrivilege(t *testing.T) {
	p := scratchPoolWith(t, Options{MaxConns: 1, StatementTimeout: 10 * time.Second})
	ctx := context.Background()
	role := "durable_probe_" + strings.ReplaceAll(t.Name(), "/", "_")
	role = strings.ToLower(role)
	if _, err := p.Exec(ctx, `CREATE ROLE `+role+` NOLOGIN`); err != nil {
		t.Skipf("cannot create a role here: %v", err)
	}
	t.Cleanup(func() { _, _ = p.Exec(context.Background(), `RESET ROLE; DROP ROLE IF EXISTS `+role) })
	if _, err := p.Exec(ctx, `SET ROLE `+role); err != nil {
		t.Fatal(err)
	}
	if err := AwaitDurable(ctx, p); err != nil {
		t.Fatalf("AwaitDurable as a role without privileges: %v", err)
	}
}
