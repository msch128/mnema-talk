//go:build integration

package webhook

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
)

// Lane 8, inventory I6 row 49: who chooses what the 5000-row total cap of the
// outgoing queue removes. doc/webhooks.md:258-260 says "the oldest are dropped
// first". The per-webhook cap is 500, a tenth of the total, so one busy webhook
// alone never reaches it; and a channel holds at most ten webhooks, so ten full
// webhooks of one channel are exactly the total. The cap bites only across
// channels. This measures what it then removes: one quiet webhook of another
// channel holds the oldest rows (a slow endpoint in backoff), the ten busy
// webhooks of a busy channel fill up behind it.
func TestTheTotalCapRemovesTheOldestRowsWhoeverOwnsThem(t *testing.T) {
	f := newFixture(t)
	d := NewDispatcher(f.p, nil, "test")
	ctx := context.Background()
	insert := func(hook, channel uuid.UUID, n int) {
		t.Helper()
		if _, err := f.p.Exec(ctx, `
			INSERT INTO outgoing_queue (webhook_id, channel_id, event_id, event_type, event_time, data)
			SELECT $1, $2, gen_random_uuid(), $3, NOW(), '{}' FROM generate_series(1, $4)`,
			hook, channel, EventMessageCreate, n); err != nil {
			t.Fatal(err)
		}
	}
	var quietCh uuid.UUID
	if err := f.p.QueryRow(ctx, `INSERT INTO channels (name, type) VALUES ('quiet', 'text') RETURNING id`).Scan(&quietCh); err != nil {
		t.Fatal(err)
	}
	quiet, _, err := createOutgoing(ctx, f.p, quietCh, f.owner.ID, "quiet", "https://quiet.example/x", []string{EventMessageCreate})
	if err != nil {
		t.Fatal(err)
	}
	const quietRows = 50
	insert(quiet.ID, quietCh, quietRows)
	busy := make([]uuid.UUID, 10)
	for i := range busy {
		busy[i] = f.outgoing(fmt.Sprintf("https://busy%d.example/x", i)).ID
		insert(busy[i], f.ch, maxPendingPerWebhook)
	}
	var before int
	if err := f.p.QueryRow(ctx, `SELECT COUNT(*) FROM outgoing_queue`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if before != quietRows+len(busy)*maxPendingPerWebhook || before <= maxPendingTotal {
		t.Fatalf("setup: %d rows, want %d (> %d)", before, quietRows+len(busy)*maxPendingPerWebhook, maxPendingTotal)
	}

	d.applyCaps(ctx)

	var after int
	if err := f.p.QueryRow(ctx, `SELECT COUNT(*) FROM outgoing_queue`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	busyLost := 0
	for _, id := range busy {
		busyLost += maxPendingPerWebhook - f.pending(id)
	}
	quietLost := quietRows - f.pending(quiet.ID)
	t.Logf("rows %d -> %d (cap %d): quiet webhook lost %d of %d, ten busy webhooks lost %d of %d",
		before, after, maxPendingTotal, quietLost, quietRows, busyLost, len(busy)*maxPendingPerWebhook)
	if after != maxPendingTotal {
		t.Fatalf("%d rows after the caps, want %d", after, maxPendingTotal)
	}
	// As documented: the oldest go, whoever owns them. The quiet webhook's
	// whole backlog is removed by the others' volume; the busy ones keep all.
	if quietLost != quietRows || busyLost != 0 {
		t.Fatalf("the total cap did not remove the oldest rows: quiet lost %d, busy lost %d", quietLost, busyLost)
	}
}
