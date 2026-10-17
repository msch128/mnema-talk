//go:build integration

package webhook

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func TestShutdownReleasesAClaimedRowAtOnce(t *testing.T) {
	f := newFixture(t)
	hook := f.outgoing("https://hooks.example/x")
	for _, query := range []string{"", "SET claimed_until = NULL WHERE seq"} {
		f.seed(hook.ID, 1)
		pool := f.p
		if query != "" { // a failing release is only logged
			pool = dbFaulty{f, query}.pool()
		}
		d := NewDispatcher(pool, loopbackClient(), "test")
		ctx, stop := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { d.work(ctx, make(chan time.Time)); close(done) }() // the rate limiter never ticks
		waitFor(t, "the claim", func() bool {
			var n int
			_ = f.p.QueryRow(context.Background(), `SELECT COUNT(*) FROM outgoing_queue WHERE claimed_until IS NOT NULL`).Scan(&n)
			return n == 1
		})
		stop()
		<-done
		var leased int
		_ = f.p.QueryRow(context.Background(), `SELECT COUNT(*) FROM outgoing_queue WHERE claimed_until IS NOT NULL`).Scan(&leased)
		if (query == "") != (leased == 0) {
			t.Fatalf("query %q: %d rows still leased", query, leased)
		}
		if _, err := f.p.Exec(context.Background(), `TRUNCATE outgoing_queue`); err != nil {
			t.Fatal(err)
		}
	}
}

func TestBufferStoreHelpers(t *testing.T) {
	f := newFixture(t)
	hook := f.outgoing("https://hooks.example/x")
	ctx := context.Background()

	// One insert takes at most storeBatch buffered events.
	d := NewDispatcher(f.p, nil, "test")
	for range storeBatch + 50 {
		d.enqueue(job{webhook: hook.ID, event: sampleEvent(), attempt: 1})
	}
	if got := d.gather(<-d.queue); len(got) != storeBatch || len(d.queue) != 50 {
		t.Fatalf("batch of %d, %d left", len(got), len(d.queue))
	}

	// Every statement of storing can fail.
	jobs := []job{{webhook: hook.ID, event: sampleEvent(), attempt: 1}}
	for _, q := range []string{"INSERT INTO outgoing_queue", "row_number()", "OFFSET"} {
		if _, _, err := storeEvents(ctx, dbFaulty{f, q}.pool(), jobs); err == nil {
			t.Errorf("storing survived a failing %q", q)
		}
	}
	// A failing step fails the recording of the attempt.
	boom := func(context.Context, pgx.Tx) error { return errors.New("boom") }
	if _, err := recordDelivery(ctx, f.p, hook.ID, Delivery{EventID: uuid.New(), EventType: EventPing, Attempt: 1}, true, true, boom); err == nil {
		t.Error("a failing step was ignored")
	}
	// Wait gives up after its bound; a failing reload is only logged.
	stuck := NewDispatcher(f.p, nil, "test")
	stuck.started.Store(true)
	stuck.Wait(10 * time.Millisecond)
	NewDispatcher(dbFaulty{f, "FROM outgoing_webhooks WHERE enabled"}.pool(), nil, "test").afterFinal(ctx, hook.ID, false)
	// Purging after a switch-off may fail: only logged.
	NewDispatcher(dbFaulty{f, "DELETE FROM outgoing_queue WHERE webhook_id"}.pool(), nil, "test").afterFinal(ctx, hook.ID, true)
}
