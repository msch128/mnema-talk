//go:build integration

package messages

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mnema-xyz/talk/internal/db"
)

// slowFlush is the test pool's database with every WAL flush held back by
// delay (commit_delay with no siblings needed): a stand-in for a slow disk.
// It needs a superuser, as the test databases have; elsewhere it skips.
func slowFlush(t *testing.T, p *db.Pool, delay time.Duration) *db.Pool {
	t.Helper()
	u, err := url.Parse(p.Config().ConnString())
	if err != nil || u.Scheme == "" {
		t.Skipf("test database address is not a URL (%v)", err)
	}
	q := u.Query() // unknown parameters are settings for the session
	q.Set("commit_delay", strconv.Itoa(int(delay/time.Microsecond)))
	q.Set("commit_siblings", "0")
	u.RawQuery = q.Encode()
	slow, err := db.ConnectWith(context.Background(), u.String(), db.Options{MaxConns: 16, StatementTimeout: 10 * time.Second})
	if err != nil {
		t.Skipf("cannot hold back WAL flushes here: %v", err)
	}
	t.Cleanup(slow.Close)
	var on, fsync string
	if err := slow.QueryRow(context.Background(), `SELECT current_setting('commit_delay'), current_setting('fsync')`).Scan(&on, &fsync); err != nil || on == "0" || fsync != "on" {
		t.Skipf("commit_delay %q, fsync %q (%v): flushes are not held back here", on, fsync, err)
	}
	return slow
}

// The posts of one channel hold its row from their number to their commit
// (insertMessageTx). They must not also queue for one another's WAL flush:
// on a slow disk that adds one flush per post ahead in the queue to every
// delivery. Each post still waits for a flush before it is answered and
// published, and concurrent posts share it.
func TestPostsInOneChannelShareTheirWALFlush(t *testing.T) {
	w := newPostWorld(t)
	const delay = 100 * time.Millisecond
	const posts = 10
	w.svc.DB = slowFlush(t, w.p, delay)
	actor := w.actor(t, "owner") // no slow mode or new-member wait in the way
	// Warm the permission cache and the connections, so only the posts are timed.
	if _, err := w.svc.Post(context.Background(), actor, PostCommand{ChannelID: w.ch, Content: "warm up"}); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	var wg sync.WaitGroup
	errs := make(chan error, posts)
	for range posts {
		wg.Go(func() {
			_, err := w.svc.Post(context.Background(), actor, PostCommand{ChannelID: w.ch, Content: "at the same time", KeepReadMarker: true})
			errs <- err
		})
	}
	wg.Wait()
	elapsed := time.Since(start)
	t.Logf("%d concurrent posts in one channel, one WAL flush %v: %v", posts, delay, elapsed)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if n := w.count(t, `SELECT COUNT(*) FROM messages WHERE channel_id = $1 AND content = 'at the same time'`, w.ch); n != posts {
		t.Fatalf("stored %d posts, want %d", n, posts)
	}
	// Answered only after a flush: at least one delay.
	if elapsed < delay {
		t.Fatalf("%d posts answered in %v, before one WAL flush (%v): not durable when answered", posts, elapsed, delay)
	}
	// Flushed one after another they take posts × delay (1 s); sharing
	// flushes, a few delays at most. (The read marker is left alone: its
	// own write would add a flush of its own to every post.)
	if limit := 6 * delay; elapsed >= limit {
		t.Fatalf("%d concurrent posts in one channel took %v (limit %v, one flush %v): they wait for each other's WAL flush", posts, elapsed, limit, delay)
	}
}

// roundTrips records the statements and batches each connection of a pool
// sends, one entry per round trip.
type roundTrips struct {
	mu   sync.Mutex
	sent map[*pgx.Conn][]string
}

func (r *roundTrips) note(c *pgx.Conn, what string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sent[c] = append(r.sent[c], what)
}

func (r *roundTrips) TraceQueryStart(ctx context.Context, c *pgx.Conn, d pgx.TraceQueryStartData) context.Context {
	r.note(c, strings.Join(strings.Fields(d.SQL), " "))
	return ctx
}
func (r *roundTrips) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}
func (r *roundTrips) TraceBatchStart(ctx context.Context, c *pgx.Conn, _ pgx.TraceBatchStartData) context.Context {
	r.note(c, "batch")
	return ctx
}
func (r *roundTrips) TraceBatchQuery(context.Context, *pgx.Conn, pgx.TraceBatchQueryData) {}
func (r *roundTrips) TraceBatchEnd(context.Context, *pgx.Conn, pgx.TraceBatchEndData)     {}

// underLock is what the connection that locked a channel sent from the lock
// to the commit, both included.
func (r *roundTrips) underLock() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, sent := range r.sent {
		for i, s := range sent {
			if strings.Contains(s, "FROM channels WHERE id = $1 FOR NO KEY UPDATE") {
				for j := i; j < len(sent); j++ {
					if sent[j] == "commit" {
						return sent[i : j+1]
					}
				}
			}
		}
	}
	return nil
}

// Every round trip while a post holds its channel's row is a wait for the
// next post in the channel (insertMessageTx). Under CPU load each costs a
// scheduling delay on both sides, so they are kept few: the lock with the
// channel's settings, the insert with its number noted, the mentioned names,
// the mentions in one batch, the stored message (the row, its details in one
// batch), the commit. (3.3.3 held it for 18.)
func TestAPostHoldsItsChannelForFewRoundTrips(t *testing.T) {
	w := newPostWorld(t)
	cfg, err := pgxpool.ParseConfig(w.p.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	trips := &roundTrips{sent: map[*pgx.Conn][]string{}}
	cfg.ConnConfig.Tracer = trips
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	w.svc.DB = &db.Pool{Pool: pool}
	post := func() {
		t.Helper()
		if _, err := w.svc.Post(context.Background(), w.actor(t, "member"), PostCommand{ChannelID: w.ch, Content: "hello @other_member"}); err != nil {
			t.Fatal(err)
		}
	}
	// The first post fills the statement cache (a statement new to a
	// connection costs a round trip of its own to prepare); the second is
	// what every later post costs.
	post()
	trips.mu.Lock()
	clear(trips.sent)
	trips.mu.Unlock()
	post()
	held := trips.underLock()
	if len(held) == 0 {
		t.Fatal("no locked section found")
	}
	const most = 7
	if len(held) > most {
		t.Fatalf("the channel row is held for %d round trips, want at most %d:\n%s", len(held), most, strings.Join(held, "\n"))
	}
}
