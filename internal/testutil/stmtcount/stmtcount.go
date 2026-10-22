//go:build integration

// Package stmtcount counts the statements a piece of code sends to Postgres,
// using pgx's supported tracing hook, so tests can assert that a batched
// operation costs the same number of round trips for one row or for many.
package stmtcount

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Counter is a pgx tracer that counts statements. Transaction control
// (BEGIN, COMMIT, ROLLBACK) is not counted.
type Counter struct{ n atomic.Int64 }

// Count returns the statements seen so far.
func (c *Counter) Count() int { return int(c.n.Load()) }

// Reset starts counting from zero.
func (c *Counter) Reset() { c.n.Store(0) }

func (c *Counter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	switch strings.ToLower(strings.TrimSpace(data.SQL)) {
	case "begin", "commit", "rollback":
	default:
		c.n.Add(1)
	}
	return ctx
}

func (*Counter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

// Pool opens a second pool on the same database as source with a counter
// attached. The pool is limited to one connection, so the count only holds
// what the test itself ran through it.
func Pool(t testing.TB, source *pgxpool.Pool) (*pgxpool.Pool, *Counter) {
	t.Helper()
	counter := &Counter{}
	config := source.Config()
	config.MaxConns, config.MinConns, config.MinIdleConns = 1, 0, 0
	config.ConnConfig.Tracer = counter
	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool, counter
}
