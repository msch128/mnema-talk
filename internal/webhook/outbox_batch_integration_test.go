//go:build integration

package webhook

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/events/wire"
	"github.com/msch128/mnema-talk/internal/testutil/stmtcount"
)

type queuedRow struct {
	webhook uuid.UUID
	name    string
	data    string
}

func (f *fixture) queued() []queuedRow {
	f.t.Helper()
	rows, err := f.p.Query(context.Background(), `SELECT webhook_id, event_type, data FROM outgoing_queue ORDER BY seq`)
	if err != nil {
		f.t.Fatal(err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (q queuedRow, err error) {
		var data []byte
		err = r.Scan(&q.webhook, &q.name, &data)
		q.data = string(data)
		return
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return out
}

// A sweep or a bulk delete removes up to 200 messages in one transaction that
// holds their row locks. Their events are one statement, and the rows are the
// ones single events make: per message in the order given, per webhook in the
// order of the ids, with the payload the WebSocket sends.
func TestTheDeletesOfABatchAreRecordedWithOneStatement(t *testing.T) {
	f := newFixture(t)
	hooks := []uuid.UUID{
		f.outgoingFor("https://hooks.example/a", EventMessageDelete).ID,
		f.outgoingFor("https://hooks.example/b", EventMessageDelete, EventMessageCreate).ID,
	}
	if hooks[0].String() > hooks[1].String() {
		hooks[0], hooks[1] = hooks[1], hooks[0]
	}
	f.outgoingFor("https://hooks.example/other", EventMessageCreate)
	d := f.attached()
	ctx := context.Background()

	const n = 200
	parent := uuid.New()
	evs := make([]events.Event, n)
	var want []queuedRow
	for i := range evs {
		del := wire.MessageDelete{ID: uuid.New(), ChannelID: f.ch}
		payload := fmt.Sprintf(`{"channel_id":"%s","id":"%s","parent_id":null}`, f.ch, del.ID)
		if i%2 == 1 {
			del.ParentID = &parent
			payload = fmt.Sprintf(`{"channel_id":"%s","id":"%s","parent_id":"%s"}`, f.ch, del.ID, parent)
		}
		evs[i] = del
		for _, hook := range hooks {
			want = append(want, queuedRow{hook, EventMessageDelete, payload})
		}
	}

	counted, counter := stmtcount.Pool(t, f.p.Pool)
	err := pgx.BeginFunc(ctx, counted, func(tx pgx.Tx) error { return d.Record(ctx, tx, f.ch, evs...) })
	if err != nil {
		t.Fatal(err)
	}
	if counter.Count() != 1 {
		t.Fatalf("%d statements for %d deletes, want 1", counter.Count(), n)
	}
	got := f.queued()
	if len(got) != len(want) {
		t.Fatalf("%d rows queued, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("row %d is %+v, want %+v", i, got[i], want[i])
		}
	}
	// They are claimed in that order too.
	for i := range want {
		if j := f.claim(); j.webhook != want[i].webhook || string(j.event.Data) != want[i].data {
			t.Fatalf("claim %d is %s for %s, want %+v", i, j.event.Data, j.webhook, want[i])
		}
	}

	// Nothing to record costs no statement.
	counter.Reset()
	if err := pgx.BeginFunc(ctx, counted, func(tx pgx.Tx) error { return d.Record(ctx, tx, f.ch) }); err != nil || counter.Count() != 0 {
		t.Fatalf("an empty batch ran %d statements (%v)", counter.Count(), err)
	}
}
