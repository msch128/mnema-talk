//go:build integration

package webhook

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Lane 7 of the defect hunt, probe of the shape of H-L7-06: recordDelivery
// updates a webhook from a subquery that takes `FOR NO KEY UPDATE` of the row,
// and its SET expressions read the counter of the outer row. A delivery that
// waited for another writer of the row must count on top of that writer's
// value, not on the value from before the wait.
func TestADeliveryThatWaitedForAnotherWriterCountsOnTopOfIt(t *testing.T) {
	f := newFixture(t)
	hook := f.outgoing("https://hooks.example/x")
	ctx := context.Background()
	if _, err := f.p.Exec(ctx, `UPDATE outgoing_webhooks SET consecutive_failures = $2 WHERE id = $1`, hook.ID, disableAfter-2); err != nil {
		t.Fatal(err)
	}
	hold, err := f.p.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = hold.Rollback(ctx) }()
	if _, err := hold.Exec(ctx, `UPDATE outgoing_webhooks SET consecutive_failures = consecutive_failures + 1 WHERE id = $1`, hook.ID); err != nil {
		t.Fatal(err)
	}
	type outcome struct {
		off  bool
		err  error
		took time.Duration
	}
	done := make(chan outcome, 1)
	go func() {
		began := time.Now()
		off, err := recordDelivery(ctx, f.p, hook.ID, Delivery{EventID: uuid.New(), EventType: EventMessageCreate, Attempt: 1}, true, true)
		done <- outcome{off, err, time.Since(began)}
	}()
	time.Sleep(500 * time.Millisecond) // the delivery reaches the row lock and waits
	if err := hold.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	got := <-done
	if got.err != nil || got.took < 400*time.Millisecond {
		t.Fatalf("delivery: err %v after %v: it did not wait, the interleaving did not happen", got.err, got.took)
	}
	var failures int
	var enabled bool
	if err := f.p.QueryRow(ctx, `SELECT consecutive_failures, enabled FROM outgoing_webhooks WHERE id = $1`, hook.ID).Scan(&failures, &enabled); err != nil {
		t.Fatal(err)
	}
	if failures != disableAfter || enabled || !got.off {
		t.Errorf("failures %d (want %d), enabled %v (want false), switched off by the call %v (want true)", failures, disableAfter, enabled, got.off)
	}
}
