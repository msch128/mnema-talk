//go:build integration

package webhook

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/metrics"
)

func ptr[T any](v T) *T { return &v }

// receiver is a webhook endpoint that counts what it gets, per delivery id.
type receiver struct {
	*httptest.Server
	mu      sync.Mutex
	byID    map[string]int
	sigTime []int64
	status  atomic.Int32
	retry   atomic.Value
	block   chan struct{} // when set, requests wait for it to close
	hits    atomic.Int32
}

func newReceiver(t *testing.T) *receiver {
	t.Helper()
	r := &receiver{byID: map[string]int{}}
	r.status.Store(http.StatusNoContent)
	r.retry.Store("")
	r.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		_, _ = io.Copy(io.Discard, req.Body)
		r.hits.Add(1)
		r.mu.Lock()
		r.byID[req.Header.Get("X-Mnema-Delivery")]++
		sig := req.Header.Get("X-Mnema-Signature") // t=<unix>,v1=...
		if ts, err := strconv.ParseInt(strings.TrimPrefix(strings.SplitN(sig, ",", 2)[0], "t="), 10, 64); err == nil {
			r.sigTime = append(r.sigTime, ts)
		}
		block := r.block
		r.mu.Unlock()
		if block != nil {
			select {
			case <-block:
			case <-req.Context().Done():
				return
			}
		}
		if v := r.retry.Load().(string); v != "" {
			w.Header().Set("Retry-After", v)
		}
		w.WriteHeader(int(r.status.Load()))
	}))
	t.Cleanup(r.Close)
	return r
}

func (r *receiver) deliveries() map[string]int {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := map[string]int{}
	for k, v := range r.byID {
		out[k] = v
	}
	return out
}

func (f *fixture) pending(hook uuid.UUID) int {
	f.t.Helper()
	var n int
	q := `SELECT COUNT(*) FROM outgoing_queue`
	var args []any
	if hook != uuid.Nil {
		q += ` WHERE webhook_id = $1`
		args = append(args, hook)
	}
	if err := f.p.QueryRow(context.Background(), q, args...).Scan(&n); err != nil {
		f.t.Fatal(err)
	}
	return n
}

// seed writes n sample events for the webhook straight into the queue.
func (f *fixture) seed(hook uuid.UUID, n int) []Event {
	f.t.Helper()
	jobs := make([]job, n)
	evs := make([]Event, n)
	for i := range jobs {
		evs[i] = sampleEvent()
		evs[i].Channel = f.ch
		jobs[i] = job{webhook: hook, event: evs[i], attempt: 1}
	}
	if _, _, err := storeEvents(context.Background(), f.p, jobs); err != nil {
		f.t.Fatal(err)
	}
	return evs
}

func (f *fixture) claim() job {
	f.t.Helper()
	j, ok, err := claimJob(context.Background(), f.p)
	if err != nil || !ok {
		f.t.Fatalf("claim: %v %v", ok, err)
	}
	return j
}

func (f *fixture) makeDue() {
	f.t.Helper()
	if _, err := f.p.Exec(context.Background(), `UPDATE outgoing_queue SET next_attempt_at = NOW(), claimed_until = NULL`); err != nil {
		f.t.Fatal(err)
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func message(ch uuid.UUID) *chat.Message {
	return &chat.Message{ID: uuid.New(), ChannelID: ch, Content: "hi"}
}

func TestProcessDeliversAndSettles(t *testing.T) {
	f := newFixture(t)
	recv := newReceiver(t)
	d := NewDispatcher(f.p, loopbackClient(), "test")
	ctx := context.Background()

	// Success: the row is gone and the attempt is logged.
	ok := f.outgoing(recv.URL)
	f.seed(ok.ID, 1)
	d.process(ctx, f.claim())
	cur, _ := getOutgoing(ctx, f.p, ok.ID)
	if cur.LastSuccessAt == nil || cur.ConsecutiveFailures != 0 || f.pending(ok.ID) != 0 {
		t.Fatalf("success state %+v, pending %d", cur.Outgoing, f.pending(ok.ID))
	}
	if log, _ := listDeliveries(ctx, f.p, ok.ID); len(log) != 1 || !log[0].Success {
		t.Fatalf("delivery log %+v", log)
	}

	// A webhook that is switched off is skipped quietly, its rows go.
	f.seed(ok.ID, 1)
	if err := updateOutgoing(ctx, f.p, ok.ID, outgoingChange{enabled: ptr(false)}); err != nil {
		t.Fatal(err)
	}
	before := recv.hits.Load()
	d.process(ctx, f.claim())
	if recv.hits.Load() != before || f.pending(ok.ID) != 0 {
		t.Fatal("a switched-off webhook was called or kept its rows")
	}

	// A failure with attempts left reschedules the same row.
	recv.status.Store(http.StatusInternalServerError)
	bad := f.outgoing(recv.URL)
	f.seed(bad.ID, 1)
	d.process(ctx, f.claim())
	var attempt int
	var in float64
	var claimed *time.Time
	row := f.p.QueryRow(ctx, `SELECT attempt, EXTRACT(EPOCH FROM next_attempt_at - NOW()), claimed_until FROM outgoing_queue WHERE webhook_id = $1`, bad.ID)
	if err := row.Scan(&attempt, &in, &claimed); err != nil || attempt != 2 || in < 3 || in > 7 || claimed != nil {
		t.Fatalf("rescheduled row: attempt %d in %.1fs claimed %v: %v", attempt, in, claimed, err)
	}
	if cur, _ := getOutgoing(ctx, f.p, bad.ID); cur.ConsecutiveFailures != 0 {
		t.Fatalf("a retry counted as a failure: %+v", cur.Outgoing)
	}

	// Retry-After is honoured (up to 10 minutes).
	recv.retry.Store("120")
	f.makeDue()
	d.process(ctx, f.claim())
	if err := f.p.QueryRow(ctx, `SELECT EXTRACT(EPOCH FROM next_attempt_at - NOW()) FROM outgoing_queue WHERE webhook_id = $1`, bad.ID).Scan(&in); err != nil || in < 100 {
		t.Fatalf("Retry-After not honoured: %.1fs %v", in, err)
	}
	recv.retry.Store("")

	// Out of attempts: the failure counts and the row is gone.
	if _, err := f.p.Exec(ctx, `UPDATE outgoing_queue SET attempt = $1, next_attempt_at = NOW()`, len(defaultBackoff)+1); err != nil {
		t.Fatal(err)
	}
	d.process(ctx, f.claim())
	if cur, _ := getOutgoing(ctx, f.p, bad.ID); cur.ConsecutiveFailures != 1 || f.pending(bad.ID) != 0 {
		t.Fatalf("final failure: %+v pending %d", cur.Outgoing, f.pending(bad.ID))
	}

	// A permanent error (410) settles at once; the fifth in a row switches
	// the webhook off and discards what else is pending for it.
	recv.status.Store(http.StatusGone)
	f.seed(bad.ID, disableAfter-1+3)
	for range disableAfter - 1 {
		d.process(ctx, f.claim())
	}
	if cur, _ := getOutgoing(ctx, f.p, bad.ID); cur.Status != "auto_disabled" || f.pending(bad.ID) != 0 {
		t.Fatalf("auto-disable: %+v pending %d", cur.Outgoing, f.pending(bad.ID))
	}

	// The webhook is deleted while its row waits: the row goes quietly.
	gone := f.outgoing(recv.URL)
	f.seed(gone.ID, 1)
	j := f.claim()
	if err := deleteOutgoing(ctx, f.p, gone.ID); err != nil {
		t.Fatal(err)
	}
	d.process(ctx, j)
	if f.pending(uuid.Nil) != 0 {
		t.Fatal("row of a deleted webhook kept")
	}
}

func TestProcessFailuresLeaveTheRow(t *testing.T) {
	f := newFixture(t)
	recv := newReceiver(t)
	recv.status.Store(http.StatusGone)
	ctx := context.Background()
	hook := f.outgoing(recv.URL)

	// Recording the attempt fails: the row stays (the lease makes it due again).
	f.seed(hook.ID, 1)
	d := NewDispatcher(dbFaulty{f, "INSERT INTO outgoing_deliveries"}.pool(), loopbackClient(), "test")
	d.process(ctx, f.claim())
	if f.pending(hook.ID) != 1 {
		t.Fatal("row lost although the attempt was not recorded")
	}
	// Loading the webhook fails.
	d = NewDispatcher(dbFaulty{f, "FROM outgoing_webhooks WHERE id"}.pool(), loopbackClient(), "test")
	f.makeDue()
	d.process(ctx, f.claim())
	if f.pending(hook.ID) != 1 {
		t.Fatal("row lost although the webhook could not be loaded")
	}
	// Dropping the row of a switched-off webhook fails: only logged.
	if err := updateOutgoing(ctx, f.p, hook.ID, outgoingChange{enabled: ptr(false)}); err != nil {
		t.Fatal(err)
	}
	d = NewDispatcher(dbFaulty{f, "DELETE FROM outgoing_queue WHERE seq"}.pool(), loopbackClient(), "test")
	f.makeDue()
	d.process(ctx, f.claim())
	// A failing reload and purge after the switch-off are logged only.
	if err := updateOutgoing(ctx, f.p, hook.ID, outgoingChange{enabled: ptr(true)}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.p.Exec(ctx, `UPDATE outgoing_webhooks SET consecutive_failures = $2 WHERE id = $1`, hook.ID, disableAfter-1); err != nil {
		t.Fatal(err)
	}
	d = NewDispatcher(dbFaulty{f, "DELETE FROM outgoing_queue WHERE webhook_id"}.pool(), loopbackClient(), "test")
	f.makeDue()
	d.process(ctx, f.claim())
	d.afterFinal(ctx, hook.ID, false)
}

func TestRestartKeepsPendingEventsAndDeliversOnce(t *testing.T) {
	f := newFixture(t)
	recv := newReceiver(t)
	hook := f.outgoing(recv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// The first process takes the event in and stops before delivering it.
	d1 := NewDispatcher(f.p, loopbackClient(), "test")
	if err := d1.Reload(ctx); err != nil {
		t.Fatal(err)
	}
	d1.Observe(f.ch, "message_create", message(f.ch))
	pctx, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { d1.persist(pctx); close(done) }()
	waitFor(t, "the event to be stored", func() bool { return f.pending(hook.ID) == 1 })
	stop()
	<-done
	if len(recv.deliveries()) != 0 {
		t.Fatal("delivered before the restart")
	}

	// The second process finds it in the database and delivers it once.
	d2 := NewDispatcher(f.p, loopbackClient(), "test")
	rctx, rstop := context.WithCancel(ctx)
	run := make(chan struct{})
	go func() { d2.Run(rctx); close(run) }()
	waitFor(t, "delivery after the restart", func() bool { return len(recv.deliveries()) == 1 && f.pending(hook.ID) == 0 })
	time.Sleep(2*d2.PollEvery + 100*time.Millisecond)
	rstop()
	<-run
	for id, n := range recv.deliveries() {
		if n != 1 {
			t.Fatalf("delivery %s made %d times", id, n)
		}
	}
	if log, _ := listDeliveries(ctx, f.p, hook.ID); len(log) != 1 || !log[0].Success || log[0].Attempt != 1 {
		t.Fatalf("delivery log %+v", log)
	}
}

// A pending retry survives a restart too and is made after the wait, with a
// fresh signature time; Run's shutdown writes what the buffer still holds.
func TestRetrySurvivesRestartAndSignsTheAttemptTime(t *testing.T) {
	f := newFixture(t)
	recv := newReceiver(t)
	recv.status.Store(http.StatusServiceUnavailable)
	hook := f.outgoing(recv.URL)
	ctx := context.Background()

	d1 := NewDispatcher(f.p, loopbackClient(), "test")
	d1.SetBackoff([]time.Duration{50 * time.Millisecond, 50 * time.Millisecond})
	d1.PollEvery = 20 * time.Millisecond
	// Shutdown before anything ran: the buffered event is still written.
	dead, kill := context.WithCancel(ctx)
	kill()
	if err := d1.Reload(ctx); err != nil {
		t.Fatal(err)
	}
	d1.Observe(f.ch, "message_create", message(f.ch))
	d1.Run(dead)
	if f.pending(hook.ID) != 1 {
		t.Fatalf("buffered event lost at shutdown: %d pending", f.pending(hook.ID))
	}
	// The event is old by the time it is sent; the signature is not.
	if _, err := f.p.Exec(ctx, `UPDATE outgoing_queue SET event_time = NOW() - INTERVAL '1 hour', created_at = NOW() - INTERVAL '1 hour'`); err != nil {
		t.Fatal(err)
	}

	d2 := NewDispatcher(f.p, loopbackClient(), "test")
	d2.SetBackoff([]time.Duration{50 * time.Millisecond, 50 * time.Millisecond})
	d2.PollEvery = 20 * time.Millisecond
	rctx, stop := context.WithCancel(ctx)
	run := make(chan struct{})
	go func() { d2.Run(rctx); close(run) }()
	waitFor(t, "the retries", func() bool { return recv.hits.Load() >= 3 && f.pending(hook.ID) == 0 })
	stop()
	<-run
	if recv.hits.Load() != 3 {
		t.Fatalf("%d attempts, want 3 (1 + 2 backoff steps)", recv.hits.Load())
	}
	recv.mu.Lock()
	for _, ts := range recv.sigTime {
		if age := time.Since(time.Unix(ts, 0)); age < -2*time.Second || age > 30*time.Second {
			t.Errorf("signature time is %v old, want the time of the attempt", age)
		}
	}
	recv.mu.Unlock()
	if cur, _ := getOutgoing(ctx, f.p, hook.ID); cur.ConsecutiveFailures != 1 {
		t.Fatalf("one event that failed for good counts once: %+v", cur.Outgoing)
	}
	if log, _ := listDeliveries(ctx, f.p, hook.ID); len(log) != 3 {
		t.Fatalf("delivery log has %d entries", len(log))
	}
}

func TestRetrySchedule(t *testing.T) {
	f := newFixture(t)
	recv := newReceiver(t)
	recv.status.Store(http.StatusBadGateway)
	hook := f.outgoing(recv.URL)
	d := NewDispatcher(f.p, loopbackClient(), "test")
	ctx := context.Background()
	f.seed(hook.ID, 1)
	for n := 1; n <= len(defaultBackoff); n++ {
		j := f.claim()
		if j.attempt != n {
			t.Fatalf("attempt %d, want %d", j.attempt, n)
		}
		d.process(ctx, j)
		var secs float64
		if err := f.p.QueryRow(ctx, `SELECT EXTRACT(EPOCH FROM next_attempt_at - NOW()) FROM outgoing_queue`).Scan(&secs); err != nil {
			t.Fatalf("attempt %d left no row: %v", n, err)
		}
		base := defaultBackoff[n-1].Seconds()
		if secs < base*0.85 || secs > base*1.15 {
			t.Fatalf("after attempt %d the next is in %.1fs, want about %.0fs", n, secs, base)
		}
		f.makeDue()
	}
	d.process(ctx, f.claim()) // the sixth and last
	if f.pending(uuid.Nil) != 0 {
		t.Fatal("row kept after the last attempt")
	}
	if cur, _ := getOutgoing(ctx, f.p, hook.ID); cur.ConsecutiveFailures != 1 {
		t.Fatalf("%+v", cur.Outgoing)
	}
	if log, _ := listDeliveries(ctx, f.p, hook.ID); len(log) != len(defaultBackoff)+1 {
		t.Fatalf("log has %d attempts", len(log))
	}
}

func TestConcurrentClaimersNeverShareARow(t *testing.T) {
	f := newFixture(t)
	hook := f.outgoing("https://hooks.example/x")
	const total = 300
	f.seed(hook.ID, total)
	var mu sync.Mutex
	seen := map[int64]int{}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				j, ok, err := claimJob(context.Background(), f.p)
				if err != nil {
					t.Error(err)
					return
				}
				if !ok {
					return
				}
				mu.Lock()
				seen[j.seq]++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if len(seen) != total {
		t.Fatalf("claimed %d of %d rows", len(seen), total)
	}
	for seq, n := range seen {
		if n != 1 {
			t.Fatalf("row %d claimed %d times", seq, n)
		}
	}
	// Claimed rows stay invisible until the lease ends.
	if _, ok, _ := claimJob(context.Background(), f.p); ok {
		t.Fatal("a leased row was claimed again")
	}
	if _, err := f.p.Exec(context.Background(), `UPDATE outgoing_queue SET claimed_until = NOW() - INTERVAL '1 second'`); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := claimJob(context.Background(), f.p); !ok {
		t.Fatal("an expired lease was not reclaimed")
	}
}

func TestTwoDispatchersDeliverEachEventOnce(t *testing.T) {
	f := newFixture(t)
	recv := newReceiver(t)
	hook := f.outgoing(recv.URL)
	const total = 40
	evs := f.seed(hook.ID, total)
	ctx, stop := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	for range 2 {
		d := NewDispatcher(f.p, loopbackClient(), "test")
		d.PollEvery = 20 * time.Millisecond
		wg.Add(1)
		go func() { defer wg.Done(); d.Run(ctx) }()
	}
	waitFor(t, "all deliveries", func() bool { return len(recv.deliveries()) == total && f.pending(hook.ID) == 0 })
	stop()
	wg.Wait()
	got := recv.deliveries()
	for _, ev := range evs {
		if got[ev.ID.String()] != 1 {
			t.Fatalf("event %s delivered %d times", ev.ID, got[ev.ID.String()])
		}
	}
}

func TestPendingCapsDropTheOldest(t *testing.T) {
	f := newFixture(t)
	d := NewDispatcher(f.p, nil, "test")
	hook := f.outgoing("https://hooks.example/x")
	ctx := context.Background()

	// Per webhook.
	before := metrics.WebhookDropped.With("webhook_cap").Load()
	evs := f.seed(hook.ID, maxPendingPerWebhook+7)
	if f.pending(hook.ID) != maxPendingPerWebhook {
		t.Fatalf("%d pending, cap %d", f.pending(hook.ID), maxPendingPerWebhook)
	}
	var oldest uuid.UUID
	if err := f.p.QueryRow(ctx, `SELECT event_id FROM outgoing_queue ORDER BY seq LIMIT 1`).Scan(&oldest); err != nil || oldest != evs[7].ID {
		t.Fatalf("oldest kept %s, want the 8th event: %v", oldest, err)
	}
	// The dispatcher counts what the caps dropped.
	d.store(ctx, []job{{webhook: hook.ID, event: sampleEvent(), attempt: 1}})
	if got := metrics.WebhookDropped.With("webhook_cap").Load() - before; got != 1 {
		t.Fatalf("webhook_cap counted %d", got)
	}
	if d.dropped.Load() != 1 || f.pending(hook.ID) != maxPendingPerWebhook {
		t.Fatalf("dropped %d, pending %d", d.dropped.Load(), f.pending(hook.ID))
	}

	// Globally: more webhooks than the total cap allows at their own caps.
	if _, err := f.p.Exec(ctx, `TRUNCATE outgoing_queue`); err != nil {
		t.Fatal(err)
	}
	hooks := maxPendingTotal/maxPendingPerWebhook + 1
	for i := range hooks {
		h := hook.ID
		if i > 0 { // a channel holds at most 10 webhooks: use one channel each
			var ch uuid.UUID
			if err := f.p.QueryRow(ctx, `INSERT INTO channels (name, type) VALUES ($1, 'text') RETURNING id`, "c"+strconv.Itoa(i)).Scan(&ch); err != nil {
				t.Fatal(err)
			}
			rec, _, err := createOutgoing(ctx, f.p, ch, f.owner.ID, "hook", "https://hooks.example/x", []string{EventMessageCreate})
			if err != nil {
				t.Fatal(err)
			}
			h = rec.ID
		}
		f.seed(h, maxPendingPerWebhook)
	}
	if got := f.pending(uuid.Nil); got != maxPendingTotal {
		t.Fatalf("%d pending, global cap %d", got, maxPendingTotal)
	}
	var minSeq int64
	if err := f.p.QueryRow(ctx, `SELECT MIN(seq) FROM outgoing_queue`).Scan(&minSeq); err != nil || minSeq <= 1 {
		t.Fatalf("oldest rows kept: %d %v", minSeq, err)
	}
	before = metrics.WebhookDropped.With("global_cap").Load()
	d.store(ctx, []job{{webhook: hook.ID, event: sampleEvent(), attempt: 1}})
	if got := metrics.WebhookDropped.With("global_cap").Load() - before; got == 0 {
		t.Fatal("global drops not counted")
	}
	// A store that fails is counted and does not panic.
	before = metrics.WebhookDropped.With("store_error").Load()
	bad := NewDispatcher(dbFaulty{f, "INSERT INTO outgoing_queue"}.pool(), nil, "test")
	bad.store(ctx, []job{{webhook: hook.ID, event: sampleEvent(), attempt: 1}})
	if metrics.WebhookDropped.With("store_error").Load()-before != 1 {
		t.Fatal("store error not counted")
	}
}

func TestPruneExpiresOldRows(t *testing.T) {
	f := newFixture(t)
	hook := f.outgoing("https://hooks.example/x")
	ctx := context.Background()
	f.seed(hook.ID, 5)
	if _, err := f.p.Exec(ctx, `UPDATE outgoing_queue SET created_at = NOW() - $1 * INTERVAL '1 second' WHERE seq IN (SELECT seq FROM outgoing_queue ORDER BY seq LIMIT 3)`, (pendingTTL + time.Minute).Seconds()); err != nil {
		t.Fatal(err)
	}
	d := NewDispatcher(f.p, nil, "test")
	before := metrics.WebhookDropped.With("expired").Load()
	d.prune(ctx)
	if f.pending(hook.ID) != 2 || metrics.WebhookDropped.With("expired").Load()-before != 3 {
		t.Fatalf("pending %d", f.pending(hook.ID))
	}
	// A failing prune is only logged.
	NewDispatcher(dbFaulty{f, "DELETE FROM outgoing_queue WHERE created_at"}.pool(), nil, "test").prune(ctx)
}

func TestShutdownLetsAttemptsFinishAndLeavesTheRest(t *testing.T) {
	f := newFixture(t)
	recv := newReceiver(t)
	recv.block = make(chan struct{})
	hook := f.outgoing(recv.URL)
	f.seed(hook.ID, 1)
	ctx, stop := context.WithCancel(context.Background())
	d := NewDispatcher(f.p, loopbackClient(), "test")
	d.PollEvery = 20 * time.Millisecond
	run := make(chan struct{})
	go func() { d.Run(ctx); close(run) }()
	waitFor(t, "the attempt to start", func() bool { return recv.hits.Load() == 1 })
	stop()
	select {
	case <-run:
		t.Fatal("Run returned while an attempt was in flight")
	case <-time.After(150 * time.Millisecond):
	}
	// The attempt finishes within the grace period: it is recorded and settled.
	close(recv.block)
	select {
	case <-run:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return")
	}
	if f.pending(hook.ID) != 0 {
		t.Fatal("finished attempt not settled")
	}
	if log, _ := listDeliveries(context.Background(), f.p, hook.ID); len(log) != 1 || !log[0].Success {
		t.Fatalf("log %+v", log)
	}
	d.Wait(time.Second) // already done
	NewDispatcher(f.p, nil, "t").Wait(time.Hour)

	// An attempt that outlasts the grace period is abandoned: nothing is
	// logged, the row is due again for the next start. Rows nobody claimed
	// stay where they are.
	old := shutdownGrace
	shutdownGrace = 50 * time.Millisecond
	defer func() { shutdownGrace = old }()
	slow := newReceiver(t)
	slow.block = make(chan struct{})
	defer close(slow.block)
	hook2 := f.outgoing(slow.URL)
	f.seed(hook2.ID, 1)
	ctx2, stop2 := context.WithCancel(context.Background())
	d2 := NewDispatcher(f.p, loopbackClient(), "test")
	d2.PollEvery = 20 * time.Millisecond
	run2 := make(chan struct{})
	go func() { d2.Run(ctx2); close(run2) }()
	waitFor(t, "the slow attempt", func() bool { return slow.hits.Load() == 1 })
	f.seed(hook2.ID, 3)
	stop2()
	select {
	case <-run2:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after the grace period")
	}
	if f.pending(hook2.ID) < 1 {
		t.Fatal("rows lost at shutdown")
	}
	if log, _ := listDeliveries(context.Background(), f.p, hook2.ID); len(log) != 0 {
		t.Fatalf("an abandoned attempt was logged: %+v", log)
	}
	var leased int
	if err := f.p.QueryRow(context.Background(), `SELECT COUNT(*) FROM outgoing_queue WHERE webhook_id = $1 AND claimed_until IS NOT NULL`, hook2.ID).Scan(&leased); err != nil || leased != 0 {
		t.Fatalf("%d rows still leased: %v", leased, err)
	}
}

func TestWorkLogsClaimErrors(t *testing.T) {
	f := newFixture(t)
	d := NewDispatcher(dbFaulty{f, "UPDATE outgoing_queue SET claimed_until = NOW()"}.pool(), loopbackClient(), "test")
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { d.work(ctx, make(chan time.Time)); close(done) }()
	time.Sleep(100 * time.Millisecond)
	stop()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not stop")
	}
}

func TestRunWorksTheQueueAndReloads(t *testing.T) {
	f := newFixture(t)
	recv := newReceiver(t)
	rec := f.outgoing(recv.URL)

	// A pool whose list query fails shows the load and reload errors; the
	// queue is still worked, and the prune and poll ticks run.
	d := NewDispatcher((&dbFaulty{f, "SELECT id, channel_id, events FROM outgoing_webhooks WHERE enabled"}).pool(), loopbackClient(), "test")
	d.ReloadEvery = 10 * time.Millisecond
	d.PollEvery = 10 * time.Millisecond
	d.PruneEvery = 10 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { d.Run(ctx); close(done) }()
	d.enqueue(job{webhook: rec.ID, event: sampleEvent(), attempt: 1})
	waitFor(t, "the buffered event to arrive", func() bool { return recv.hits.Load() == 1 })
	time.Sleep(50 * time.Millisecond) // a few reload ticks
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not stop")
	}
	// A full buffer drops events instead of blocking.
	full := NewDispatcher(f.p, loopbackClient(), "test")
	channels := make([]uuid.UUID, 8)
	for i := range channels {
		channels[i] = uuid.New()
	}
	for i := range queueSize + 205 {
		ev := sampleEvent()
		ev.Channel = channels[i%len(channels)]
		full.enqueue(job{webhook: rec.ID, event: ev, attempt: 1})
	}
	if full.dropped.Load() != 205 {
		t.Fatalf("dropped %d", full.dropped.Load())
	}
}
