//go:build integration

package server

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/chat/channels"
	"github.com/msch128/mnema-talk/internal/db"
)

// beforeStatement runs fn once, before the first statement containing match
// is sent, on the connection's goroutine (the statement waits for it).
type beforeStatement struct {
	match string
	once  sync.Once
	fn    func()
	hit   bool
}

func (b *beforeStatement) TraceQueryStart(ctx context.Context, _ *pgx.Conn, d pgx.TraceQueryStartData) context.Context {
	if strings.Contains(strings.Join(strings.Fields(d.SQL), " "), b.match) {
		b.once.Do(func() { b.hit = true; b.fn() })
	}
	return ctx
}

func (*beforeStatement) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func hookedPool(t *testing.T, source *db.Pool, hook *beforeStatement) *db.Pool {
	t.Helper()
	config := source.Config().Copy()
	config.ConnConfig.Tracer = hook
	config.MinConns = 0
	p, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	t.Cleanup(func() {
		if !hook.hit {
			t.Errorf("the hook never saw %q", hook.match)
		}
	})
	return &db.Pool{Pool: p}
}

// Lane 7 of the defect hunt. Auto-delete keeps "a thread root that still has
// a reply younger than the cutoff or a pinned one" (sweepBatch). A reply that
// is pinned after the sweep chose its root and before the root's delete
// cascades to it must stay, or the pin must wait for the sweep.
func TestAReplyPinnedDuringTheSweepIsNotDeletedWithItsRoot(t *testing.T) {
	f := newAutoDeleteFixture(t)
	wantStatus(t, "auto-delete", f.set(f.owner, f.channel, 7), http.StatusOK)
	root := f.a.send(f.alice, f.channel, "old root")
	res := f.bob.post(f.base()+"/messages", map[string]any{"content": "old reply", "parent_id": root})
	wantStatus(t, "reply", res, http.StatusCreated)
	var reply struct{ ID string }
	res.decode(t, &reply)
	replyID := uuid.MustParse(reply.ID)
	f.age(t, root, 30)
	f.age(t, replyID, 30)

	pinned := make(chan int, 1)
	hook := &beforeStatement{match: "DELETE FROM messages WHERE id = ANY", fn: func() {
		go func() {
			pinned <- f.owner.do(http.MethodPut, f.base()+"/messages/"+replyID.String()+"/pin", nil, nil).status
		}()
		select {
		case status := <-pinned:
			pinned <- status
		case <-time.After(3 * time.Second):
			// The pin waits for the sweep: serialised, which is fine.
		}
	}}
	d := &channels.AutoDeleter{DB: hookedPool(t, f.a.db, hook), Events: f.a.events, Objects: f.a.store, Audit: &audit.Log{DB: f.a.db}}
	if err := d.Sweep(t.Context()); err != nil {
		t.Fatal(err)
	}
	status := <-pinned
	t.Logf("pin during the sweep answered %d; reply kept %v, root kept %v", status, f.exists(replyID), f.exists(root))
	if status == http.StatusOK && !f.exists(replyID) {
		t.Errorf("the pin answered 200 during the sweep, and the pinned reply was deleted with its root")
	}
	if status == http.StatusOK && !f.exists(root) {
		t.Logf("root deleted although a reply of it is pinned")
	}
}
