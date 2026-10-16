//go:build integration

// Package pgfault places a real context cancellation at a chosen database
// operation using pgx's supported tracing hooks. The pool, rows, errors and
// production code stay real; only the request context is canceled at the
// boundary a test names. It does not import internal/db so every package's
// tests (db included) can use it.
package pgfault

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Tracer cancels the context of the first statement containing Query. With
// AfterPrepare the cancellation lands once the statement is prepared, i.e.
// after Query succeeded and before its rows stream, which surfaces as a
// rows.Err() failure instead of a Query error.
type Tracer struct {
	Query        string
	AfterPrepare bool

	mu        sync.Mutex
	cancel    context.CancelFunc
	preparing bool
	hit       bool
}

// Hit reports whether a statement matching Query was traced.
func (t *Tracer) Hit() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.hit
}

func (t *Tracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if !strings.Contains(normalize(data.SQL), normalize(t.Query)) {
		return ctx
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.hit = true
	ctx, t.cancel = context.WithCancel(ctx)
	if !t.AfterPrepare {
		t.cancel()
	}
	return ctx
}

func (*Tracer) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func (t *Tracer) TracePrepareStart(ctx context.Context, _ *pgx.Conn, data pgx.TracePrepareStartData) context.Context {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.preparing = strings.Contains(normalize(data.SQL), normalize(t.Query))
	return ctx
}

func (t *Tracer) TracePrepareEnd(context.Context, *pgx.Conn, pgx.TracePrepareEndData) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.AfterPrepare && t.preparing && t.cancel != nil {
		t.cancel()
	}
}

func normalize(sql string) string { return strings.Join(strings.Fields(sql), " ") }

// Pool opens a second pool on the same database as source with the tracer
// attached, and fails the test when the tracer never saw its statement. The
// pool is limited to one connection so the tracer's state is not shared.
func Pool(t testing.TB, source *pgxpool.Pool, tracer *Tracer) *pgxpool.Pool {
	t.Helper()
	config := source.Config()
	config.MaxConns, config.MinConns, config.MinIdleConns = 1, 0, 0
	config.ConnConfig.Tracer = tracer
	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	t.Cleanup(func() {
		if !tracer.Hit() {
			t.Errorf("fault tracer never saw a statement containing %q", tracer.Query)
		}
	})
	return pool
}
