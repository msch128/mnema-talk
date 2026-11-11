//go:build integration

package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/chat/messages"
	"github.com/msch128/mnema-talk/internal/events/wire"
)

// bodies is a receiver that keeps what it gets, in the order of arrival.
type bodies struct {
	*httptest.Server
	mu  sync.Mutex
	got []Envelope
}

func newBodies(t *testing.T) *bodies {
	t.Helper()
	b := &bodies{}
	b.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var env Envelope
		if err := json.Unmarshal(raw, &env); err != nil {
			t.Errorf("delivery is not an envelope: %s", raw)
		}
		b.mu.Lock()
		b.got = append(b.got, env)
		b.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(b.Close)
	return b
}

func (b *bodies) all() []Envelope {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]Envelope(nil), b.got...)
}

// attached returns a dispatcher that knows the fixture's webhooks and is the
// outbox of the fixture's database, as the server wires it. It does not run.
func (f *fixture) attached() *Dispatcher {
	f.t.Helper()
	d := NewDispatcher(f.p, loopbackClient(), "test")
	if err := d.Reload(context.Background()); err != nil {
		f.t.Fatal(err)
	}
	core.AttachOutbox(f.p, d)
	f.t.Cleanup(func() { core.DetachOutbox(f.p, d) })
	return d
}

// post stores a message the way every sender does.
func (f *fixture) post(content string, also func(pgx.Tx, uuid.UUID) error) (uuid.UUID, error) {
	return messages.InsertMessage(context.Background(), f.p, nil, messages.NewMessage{ChannelID: f.ch, UserID: f.owner.ID, Content: content}, also)
}

// run starts a dispatcher until the test ends.
func (f *fixture) run(d *Dispatcher) {
	f.t.Helper()
	d.PollEvery = 20 * time.Millisecond
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { d.Run(ctx); close(done) }()
	f.t.Cleanup(func() { stop(); <-done })
}

// The process dies right after the message's transaction committed: nothing
// was published to the hub and no worker ran. The event is in the database
// all the same, and the next start delivers it, once, with the message as it
// was stored.
func TestAMessageEventSurvivesACrashBetweenCommitAndDispatch(t *testing.T) {
	f := newFixture(t)
	recv := newBodies(t)
	hook := f.outgoing(recv.URL)
	f.attached() // the first process: it never publishes and never runs

	id, err := f.post("written just before the crash", nil)
	if err != nil {
		t.Fatal(err)
	}
	if f.pending(hook.ID) != 1 {
		t.Fatalf("%d events pending after the commit, want 1", f.pending(hook.ID))
	}

	f.run(NewDispatcher(f.p, loopbackClient(), "test")) // the next start
	waitFor(t, "the delivery after the restart", func() bool { return len(recv.all()) == 1 && f.pending(hook.ID) == 0 })
	time.Sleep(100 * time.Millisecond)
	got := recv.all()
	if len(got) != 1 || got[0].T != EventMessageCreate {
		t.Fatalf("deliveries after the restart: %+v", got)
	}
	msg, err := messages.GetMessage(context.Background(), f.p, id)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := json.Marshal(msg)
	if !bytes.Equal(got[0].D, want) {
		t.Fatalf("payload\n got %s\nwant %s", got[0].D, want)
	}
}

// The event belongs to the message's transaction: when that fails, nothing is
// left to deliver.
func TestAMessageThatIsNotStoredLeavesNoEvent(t *testing.T) {
	f := newFixture(t)
	hook := f.outgoing("https://hooks.example/x")
	f.attached()
	boom := errors.New("the attachment could not be recorded")
	if _, err := f.post("never stored", func(pgx.Tx, uuid.UUID) error { return boom }); !errors.Is(err, boom) {
		t.Fatalf("post: %v", err)
	}
	if f.pending(hook.ID) != 0 {
		t.Fatalf("%d events pending for a message that was rolled back", f.pending(hook.ID))
	}
}

// The normal path: the transaction writes the event, then the hub publishes
// it. The publication must not queue it a second time.
func TestAPublishedMessageIsDeliveredOnce(t *testing.T) {
	f := newFixture(t)
	recv := newBodies(t)
	hook := f.outgoing(recv.URL)
	d := f.attached()
	f.run(d)

	for _, content := range []string{"one", "two", "three"} {
		id, err := f.post(content, nil)
		if err != nil {
			t.Fatal(err)
		}
		msg, err := messages.GetMessage(context.Background(), f.p, id)
		if err != nil {
			t.Fatal(err)
		}
		d.Observe(f.ch, (*core.MessageCreate)(msg)) // what the hub's tap does after the commit
	}
	waitFor(t, "the deliveries", func() bool { return len(recv.all()) >= 3 && f.pending(hook.ID) == 0 })
	time.Sleep(5 * d.PollEvery)
	seen := map[string]int{}
	for _, env := range recv.all() {
		seen[env.ID]++
	}
	if len(recv.all()) != 3 || len(seen) != 3 {
		t.Fatalf("3 messages made %d deliveries of %d events", len(recv.all()), len(seen))
	}
}

// A webhook's events are claimed in the order their transactions committed:
// the message, its edit, a reaction, its deletion.
func TestEventsOfAWebhookKeepTheOrderOfTheirCommits(t *testing.T) {
	f := newFixture(t)
	f.outgoingFor("https://hooks.example/x", EventMessageCreate, EventMessageUpdate, EventReactionsUpdate, EventMessageDelete)
	f.attached()
	ctx := context.Background()

	first, err := f.post("first", nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.post("second", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := messages.EditMessage(ctx, f.p, nil, first, f.owner.ID, "first, edited"); err != nil {
		t.Fatal(err)
	}
	if _, err := messages.ToggleReactionAs(ctx, f.p, second, f.owner.ID, "👍", true); err != nil {
		t.Fatal(err)
	}
	if _, err := messages.DeleteMessageAs(ctx, f.p, first, f.owner, true); err != nil {
		t.Fatal(err)
	}

	want := []struct {
		event   string
		message uuid.UUID
	}{
		{EventMessageCreate, first}, {EventMessageCreate, second}, {EventMessageUpdate, first},
		{EventReactionsUpdate, second}, {EventMessageDelete, first},
	}
	for i, w := range want {
		j := f.claim()
		var d struct {
			ID        uuid.UUID `json:"id"`
			MessageID uuid.UUID `json:"message_id"`
		}
		if err := json.Unmarshal(j.event.Data, &d); err != nil {
			t.Fatal(err)
		}
		if d.ID == uuid.Nil {
			d.ID = d.MessageID
		}
		if j.event.Type != w.event || d.ID != w.message {
			t.Fatalf("claim %d is %s of %s, want %s of %s", i, j.event.Type, d.ID, w.event, w.message)
		}
	}
	if _, ok, _ := claimJob(ctx, f.p); ok {
		t.Fatal("more events than changes")
	}
}

// Two changes race: the one whose transaction began first is written and
// committed last (an edit that waited for the other's row lock). The queue
// hands them out in the order they were written, so a receiver ends on the
// state the database ends on; the start of a transaction decides nothing.
func TestAnEventIsClaimedInTheOrderItWasWrittenNotTheOrderItsTransactionBegan(t *testing.T) {
	f := newFixture(t)
	f.outgoingFor("https://hooks.example/x", EventMessageDelete)
	d := f.attached()
	ctx := context.Background()
	early, late := wire.MessageDelete{ID: uuid.New(), ChannelID: f.ch}, wire.MessageDelete{ID: uuid.New(), ChannelID: f.ch}

	slow, err := f.p.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = slow.Rollback(ctx) }()
	var began time.Time
	if err := slow.QueryRow(ctx, `SELECT NOW()`).Scan(&began); err != nil { // the transaction has begun
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	if err := pgx.BeginFunc(ctx, f.p, func(tx pgx.Tx) error { return d.Record(ctx, tx, f.ch, early) }); err != nil {
		t.Fatal(err)
	}
	if err := d.Record(ctx, slow, f.ch, late); err != nil {
		t.Fatal(err)
	}
	if err := slow.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	for i, want := range []uuid.UUID{early.ID, late.ID} {
		var got wire.MessageDelete
		if err := json.Unmarshal(f.claim().event.Data, &got); err != nil {
			t.Fatal(err)
		}
		if got.ID != want {
			t.Fatalf("claim %d is the event written %s", i, map[bool]string{true: "first", false: "last"}[got.ID == early.ID])
		}
	}
}

// A webhook that was deleted while the list of webhooks was still cached must
// not fail the message that would have gone to it.
func TestAMessageIsStoredWhenItsWebhookJustWentAway(t *testing.T) {
	f := newFixture(t)
	gone := f.outgoing("https://hooks.example/gone")
	off := f.outgoing("https://hooks.example/off")
	f.attached() // caches both
	ctx := context.Background()
	if _, err := f.p.Exec(ctx, `DELETE FROM outgoing_webhooks WHERE id = $1`, gone.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.p.Exec(ctx, `UPDATE outgoing_webhooks SET enabled = FALSE WHERE id = $1`, off.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.post("still stored", nil); err != nil {
		t.Fatalf("post with a stale list of webhooks: %v", err)
	}
	var n int
	if err := f.p.QueryRow(ctx, `SELECT COUNT(*) FROM outgoing_queue`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("%d events queued for webhooks that are gone or off (%v)", n, err)
	}
}

// The bounds of the queue are applied by the dispatcher, not by the
// transaction of the message: past them the oldest events go.
func TestTheDispatcherBoundsWhatTransactionsWrote(t *testing.T) {
	f := newFixture(t)
	hook := f.outgoing("https://hooks.example/x")
	d := f.attached()
	ctx := context.Background()
	if _, err := f.p.Exec(ctx, `
		INSERT INTO outgoing_queue (webhook_id, channel_id, event_id, event_type, event_time, data)
		SELECT $1, $2, gen_random_uuid(), $3, NOW(), '{}' FROM generate_series(1, $4)`,
		hook.ID, f.ch, EventMessageCreate, maxPendingPerWebhook+7); err != nil {
		t.Fatal(err)
	}
	newest, err := f.post("the newest", nil)
	if err != nil {
		t.Fatal(err)
	}
	d.applyCaps(ctx)
	if f.pending(hook.ID) != maxPendingPerWebhook {
		t.Fatalf("%d pending after the caps, want %d", f.pending(hook.ID), maxPendingPerWebhook)
	}
	var data []byte
	if err := f.p.QueryRow(ctx, `SELECT data FROM outgoing_queue ORDER BY seq DESC LIMIT 1`).Scan(&data); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(newest.String())) {
		t.Fatal("the caps dropped the newest event instead of the oldest")
	}
	// Nothing new since: the next look costs no statement.
	if d.capsDue.Load() {
		t.Fatal("caps still due after they were applied")
	}
	// A failing statement leaves them due.
	for _, q := range []string{"row_number()", "OFFSET"} {
		broken := NewDispatcher(dbFaulty{f, q}.pool(), nil, "test")
		broken.applyCaps(ctx)
		if !broken.capsDue.Load() {
			t.Errorf("caps not due again after a failing %q", q)
		}
	}
}

// The event and the message are one transaction: when the event cannot be
// written the message is not stored either, and the sender sees the failure.
func TestAMessageWhoseEventCannotBeWrittenIsNotStored(t *testing.T) {
	f := newFixture(t)
	f.outgoing("https://hooks.example/x")
	broken := dbFaulty{f, "INSERT INTO outgoing_queue"}.pool()
	d := NewDispatcher(broken, nil, "test")
	if err := d.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	core.AttachOutbox(broken, d)
	t.Cleanup(func() { core.DetachOutbox(broken, d) })
	_, err := messages.InsertMessage(context.Background(), broken, nil, messages.NewMessage{ChannelID: f.ch, UserID: f.owner.ID, Content: "x"}, nil)
	if err == nil {
		t.Fatal("the message was stored without its event")
	}
	var n int
	if err := f.p.QueryRow(context.Background(), `SELECT COUNT(*) FROM messages`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("%d messages stored (%v)", n, err)
	}
}
