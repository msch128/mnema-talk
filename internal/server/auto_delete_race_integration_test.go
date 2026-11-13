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
// is sent, on the connection's goroutine (the statement waits for it). With
// after set, fn runs once that statement has ended instead (for a query: its
// rows are closed), before the next one is sent.
type beforeStatement struct {
	match string
	after bool
	once  sync.Once
	fn    func()
	hit   bool
}

type matchedStatement struct{}

func (b *beforeStatement) TraceQueryStart(ctx context.Context, _ *pgx.Conn, d pgx.TraceQueryStartData) context.Context {
	if !strings.Contains(strings.Join(strings.Fields(d.SQL), " "), b.match) {
		return ctx
	}
	if b.after {
		return context.WithValue(ctx, matchedStatement{}, true)
	}
	b.once.Do(func() { b.hit = true; b.fn() })
	return ctx
}

func (b *beforeStatement) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryEndData) {
	if ctx.Value(matchedStatement{}) != nil {
		b.once.Do(func() { b.hit = true; b.fn() })
	}
}

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

// Lane 7 of the defect hunt (H-L7-09). Auto-delete keeps "a thread root that
// still has a reply younger than the cutoff or a pinned one" (sweepBatch). The
// pin goes through the route and is in flight when the sweep selects: it has
// changed the reply and holds its row lock, but has not committed (its foreign
// key check on pinned_by waits for the user row the test holds). Since 0105
// the sweep's batch share-locks the channel row before it selects, and the pin
// holds that row: the user row is let go when the sweep reaches that lock, the
// pin commits, and the batch then sees the pinned reply. The pinned reply and
// its root must stay; an unrelated old message shows that the sweep ran.
func TestAReplyPinnedDuringTheSweepIsNotDeletedWithItsRoot(t *testing.T) {
	f := newAutoDeleteFixture(t)
	wantStatus(t, "auto-delete", f.set(f.owner, f.channel, 7), http.StatusOK)
	root := f.a.send(f.alice, f.channel, "old root")
	reply := f.replyTo(t, root)
	other := f.a.send(f.alice, f.channel, "old message")
	for _, id := range []uuid.UUID{root, reply, other} {
		f.age(t, id, 30)
	}

	ctx := t.Context()
	hold, err := f.a.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = hold.Rollback(ctx) }()
	if _, err := hold.Exec(ctx, `SELECT 1 FROM users WHERE id = $1 FOR UPDATE`, f.owner.user.ID); err != nil {
		t.Fatal(err)
	}
	pinned := make(chan int, 1)
	go func() {
		pinned <- f.owner.do(http.MethodPut, f.base()+"/messages/"+reply.String()+"/pin", nil, nil).status
	}()
	f.waitForThePinsUpdate(t)

	status := 0
	hook := &beforeStatement{match: "SELECT 1 FROM channels WHERE id = $1 FOR SHARE", fn: func() {
		if err := hold.Commit(ctx); err != nil {
			t.Error(err)
		}
		select {
		case status = <-pinned:
		case <-time.After(10 * time.Second):
			t.Error("the pin did not answer once the user row was free")
		}
	}}
	d := &channels.AutoDeleter{DB: hookedPool(t, f.a.db, hook), Events: f.a.events, Objects: f.a.store, Audit: &audit.Log{DB: f.a.db}}
	if err := d.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK {
		t.Fatalf("the pin answered %d, want 200", status)
	}
	if f.exists(other) {
		t.Fatal("the unrelated old message was not deleted: the sweep did not run")
	}
	kept := f.a.countRows(`SELECT COUNT(*) FROM messages WHERE id = $1 AND is_pinned`, reply) == 1
	if !kept || !f.exists(root) {
		t.Errorf("the pin answered 200 during the sweep: pinned reply kept %v, root kept %v", kept, f.exists(root))
	}
}

// waitForThePinsUpdate waits until the pin's UPDATE waits for a lock: the
// reply's row is then changed and locked by the uncommitted pin.
func (f autoDeleteFixture) waitForThePinsUpdate(t *testing.T) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if f.a.countRows(`SELECT COUNT(*) FROM pg_stat_activity WHERE datname = current_database() AND wait_event_type = 'Lock' AND query LIKE '%UPDATE messages SET is_pinned%'`) == 1 {
			return
		}
	}
	t.Fatal("the pin never reached its UPDATE")
}
