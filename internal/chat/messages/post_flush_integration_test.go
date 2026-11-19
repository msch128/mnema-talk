//go:build integration

package messages

import (
	"context"
	"net/url"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/msch128/mnema-talk/internal/db"
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
