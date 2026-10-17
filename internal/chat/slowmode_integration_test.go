//go:build integration

package chat

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestSlowModePostTimesArePruned(t *testing.T) {
	f := newChatFixture(t)
	ctx := context.Background()
	var other = f.u.ID
	if err := f.p.QueryRow(ctx, `INSERT INTO users (username, display_name, password_hash) VALUES ('other','Other','x') RETURNING id`).Scan(&other); err != nil {
		t.Fatal(err)
	}
	if _, err := f.p.Exec(ctx, `INSERT INTO slow_mode_posts (channel_id, user_id, last_post_at) VALUES ($1, $2, NOW() - INTERVAL '7 hours'), ($1, $3, NOW() - INTERVAL '1 minute')`, f.c.ID, f.u.ID, other); err != nil {
		t.Fatal(err)
	}
	if n, err := PruneSlowMode(ctx, f.p); err != nil || n != 1 {
		t.Fatalf("pruned %d, %v", n, err)
	}

	// The loop prunes on its tick and stops with its context.
	if _, err := f.p.Exec(ctx, `UPDATE slow_mode_posts SET last_post_at = NOW() - INTERVAL '8 hours'`); err != nil {
		t.Fatal(err)
	}
	rctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { RunSlowModePrune(rctx, f.p, 5*time.Millisecond); close(done) }()
	deadline := time.Now().Add(2 * time.Second)
	for {
		var left int
		_ = f.p.QueryRow(ctx, `SELECT COUNT(*) FROM slow_mode_posts`).Scan(&left)
		if left == 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
	var left int
	if err := f.p.QueryRow(ctx, `SELECT COUNT(*) FROM slow_mode_posts`).Scan(&left); err != nil || left != 0 {
		t.Fatalf("rows left %d, %v", left, err)
	}

	// A failing prune is an error (the loop only logs it).
	cctx, ccancel := context.WithCancel(ctx)
	ccancel()
	if _, err := PruneSlowMode(cctx, f.p); err == nil {
		t.Fatal("prune on a cancelled context succeeded")
	}
	ectx, ecancel := context.WithCancel(ctx)
	go func() { time.Sleep(20 * time.Millisecond); ecancel() }()
	RunSlowModePrune(ectx, closedQuerier{}, time.Millisecond)
}

// closedQuerier fails every statement.
type closedQuerier struct{}

func (closedQuerier) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errors.New("database is gone")
}
func (closedQuerier) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("database is gone")
}
func (closedQuerier) QueryRow(context.Context, string, ...any) pgx.Row { return nil }
