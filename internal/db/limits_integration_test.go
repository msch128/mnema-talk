//go:build integration

package db

import (
	"context"
	"testing"
	"testing/fstest"
	"time"
)

// shortLimit is a pool whose statements may run 200 ms, on one connection so
// every statement of a test meets the same session.
func shortLimit(t *testing.T) *Pool {
	t.Helper()
	return scratchPoolWith(t, Options{MaxConns: 1, StatementTimeout: 200 * time.Millisecond})
}

func TestPoolCancelsAStatementOverTheLimit(t *testing.T) {
	p := shortLimit(t)
	_, err := p.Exec(context.Background(), `SELECT pg_sleep(2)`)
	if !IsStatementTimeout(err) {
		t.Fatalf("a statement ten times over the limit ended with %v, want a statement timeout", err)
	}
	if _, err := p.Exec(context.Background(), `SELECT pg_sleep(0.01)`); err != nil {
		t.Fatalf("a short statement after the cancelled one: %v", err)
	}
}

func TestBulkWorkGetsItsLongerLimitAndHandsTheConnectionBackShort(t *testing.T) {
	p := shortLimit(t)
	ctx := context.Background()
	bulk := WithStatementTimeout(ctx, BulkStatementTimeout)
	if _, err := p.Exec(bulk, `SELECT pg_sleep(0.6)`); err != nil {
		t.Fatalf("bulk statement over the pool limit: %v", err)
	}
	var limit string
	if err := p.QueryRow(bulk, `SHOW statement_timeout`).Scan(&limit); err != nil || limit != "5min" {
		t.Fatalf("limit of bulk work %q (%v), want 5min", limit, err)
	}
	// The pool has one connection: the next ordinary caller gets the one
	// that just ran bulk work.
	if err := p.QueryRow(ctx, `SHOW statement_timeout`).Scan(&limit); err != nil || limit != "200ms" {
		t.Fatalf("limit after bulk work %q (%v), want 200ms", limit, err)
	}
	if _, err := p.Exec(ctx, `SELECT pg_sleep(2)`); !IsStatementTimeout(err) {
		t.Fatalf("an ordinary statement after bulk work ended with %v, want a statement timeout", err)
	}
}

func TestMigrationLongerThanTheLimitStillApplies(t *testing.T) {
	p := shortLimit(t)
	ctx := context.Background()
	slow := fstest.MapFS{"0001_slow.sql": {Data: []byte(`CREATE TABLE slow_built AS SELECT pg_sleep(0.6)::text AS waited;`)}}
	if err := p.MigrateFS(ctx, slow); err != nil {
		t.Fatalf("a migration three times over the statement limit: %v", err)
	}
	var applied bool
	if err := p.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE filename = '0001_slow.sql')`).Scan(&applied); err != nil || !applied {
		t.Fatalf("slow migration recorded = %v (%v)", applied, err)
	}
}

func TestPoolEndsSessionsIdleInATransaction(t *testing.T) {
	p := scratchPool(t)
	var statement, idle string
	err := p.QueryRow(context.Background(), `SELECT current_setting('statement_timeout'), current_setting('idle_in_transaction_session_timeout')`).Scan(&statement, &idle)
	if err != nil || statement != "15s" || idle != "1min" {
		t.Fatalf("default limits: statement %q, idle in transaction %q (%v), want 15s and 1min", statement, idle, err)
	}
}

func TestPoolSizeAndUnlimitedStatements(t *testing.T) {
	p := scratchPoolWith(t, Options{MaxConns: 3, StatementTimeout: 0})
	if got := p.Config().MaxConns; got != 3 {
		t.Errorf("pool size %d, want 3", got)
	}
	var limit string
	if err := p.QueryRow(WithStatementTimeout(context.Background(), time.Minute), `SHOW statement_timeout`).Scan(&limit); err != nil || limit != "0" {
		t.Errorf("limit of a pool without one %q (%v), want 0", limit, err)
	}
}
