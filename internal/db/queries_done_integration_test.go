//go:build integration

package db

import (
	"context"
	"testing"
	"time"
)

// QueriesDone counts finished work only. A query cancelled against a database
// that does not answer destroys its connection, and the pool counters still
// move (handed out, not held): GET /api/health must not read that as a busy
// database, so it watches QueriesDone instead.
func TestQueriesDoneCountsOnlyQueriesThatFinished(t *testing.T) {
	p := scratchPool(t)
	ctx := context.Background()
	before, stats := p.QueriesDone(), p.Snapshot()
	for range 3 {
		cctx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
		_, err := p.Exec(cctx, `SELECT pg_sleep(5)`)
		cancel()
		if err == nil {
			t.Fatal("a cancelled query finished")
		}
	}
	after := p.Snapshot()
	if moved := (after.AcquireCount - int64(after.Acquired)) - (stats.AcquireCount - int64(stats.Acquired)); moved < 2 {
		t.Fatalf("pool counters moved by %d for 3 cancelled queries; the test no longer shows the trap", moved)
	}
	if got := p.QueriesDone() - before; got != 0 {
		t.Fatalf("%d queries counted as done, none finished", got)
	}
	for range 2 {
		if _, err := p.Exec(ctx, `SELECT 1`); err != nil {
			t.Fatal(err)
		}
	}
	if got := p.QueriesDone() - before; got != 2 {
		t.Fatalf("%d queries counted as done, want 2", got)
	}
}
