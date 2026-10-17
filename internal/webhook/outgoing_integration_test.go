//go:build integration

package webhook

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/ssrf"
	"github.com/msch128/mnema-talk/internal/testutil/pgfault"
)

// loopbackClient reaches the test receivers on the loopback interface.
func loopbackClient() *http.Client {
	allow, _ := ssrf.ParseAllow("127.0.0.0/8")
	return ssrf.NewClient(ssrf.NewDialer(&ssrf.Policy{Allow: allow}), time.Second)
}

func (f *fixture) outgoing(url string) *outgoingRec {
	f.t.Helper()
	rec, _, err := createOutgoing(context.Background(), f.p, f.ch, f.owner.ID, "hook", url, []string{EventMessageCreate})
	if err != nil {
		f.t.Fatal(err)
	}
	return rec
}

func sampleEvent() Event {
	return Event{ID: uuid.New(), Type: EventMessageCreate, Time: time.Now().UTC(), Data: []byte(`{"id":"x"}`)}
}

// Every statement of the outgoing management API can fail; the answer is a
// sanitized 500.
func TestOutgoingManagementDatabaseFailures(t *testing.T) {
	f := newFixture(t)
	rec := f.outgoing("https://hooks.example/x")
	base := "/admin/outgoing-webhooks/" + rec.ID.String()
	list := "/admin/channels/" + f.ch.String() + "/outgoing-webhooks"
	body := `{"name":"n","url":"https://hooks.example/y","events":["MESSAGE_CREATE"]}`

	cases := []struct {
		name, method, path, body, query string
		afterPrepare                    bool
		skip                            int
		want                            int
	}{
		{"list query", "GET", list, "", "FROM outgoing_webhooks WHERE channel_id = $1", false, 0, 500},
		{"list stream", "GET", list, "", "FROM outgoing_webhooks WHERE channel_id = $1", true, 0, 500},
		{"create count", "POST", list, body, "SELECT COUNT(*) FROM outgoing_webhooks", false, 0, 500},
		{"create insert", "POST", list, body, "INSERT INTO outgoing_webhooks", false, 0, 500},
		{"create read-back", "POST", list, body, "FROM outgoing_webhooks WHERE id = $1", false, 0, 500},
		{"load", "PATCH", base, `{"name":"x"}`, "FROM outgoing_webhooks WHERE id = $1", false, 0, 500},
		{"update", "PATCH", base, `{"name":"x"}`, "UPDATE outgoing_webhooks SET name", false, 0, 500},
		{"update read-back", "PATCH", base, `{"name":"x"}`, "FROM outgoing_webhooks WHERE id = $1", false, 1, 500},
		{"rotate", "POST", base + "/secret", "", "SET secret", false, 0, 500},
		{"deliveries", "GET", base + "/deliveries", "", "FROM outgoing_deliveries", false, 0, 500},
		{"delete", "DELETE", base, "", "DELETE FROM outgoing_webhooks", false, 0, 500},
		{"test record", "POST", base + "/test", "", "INSERT INTO outgoing_deliveries", false, 0, 500},
		{"test prune", "POST", base + "/test", "", "DELETE FROM outgoing_deliveries", false, 0, 500},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := f.faulty(&pgfault.Tracer{Query: tc.query, AfterPrepare: tc.afterPrepare, Skip: tc.skip})
			var w *httptest.ResponseRecorder
			if tc.body == "" {
				w = f.serve(h, tc.method, tc.path, nil, nil)
			} else {
				w = f.json(h, tc.method, tc.path, tc.body)
			}
			wantStatus(t, w, tc.want, tc.name)
			if strings.Contains(w.Body.String(), "cancel") {
				t.Errorf("database error leaked: %s", w.Body)
			}
		})
	}
	if _, err := getOutgoing(context.Background(), f.p, rec.ID); err != nil {
		t.Fatalf("webhook damaged by failed operations: %v", err)
	}
}

// Losing the list of webhooks after a change is logged, not fatal.
func TestReloadFailureAfterChangeIsNotFatal(t *testing.T) {
	f := newFixture(t)
	h := f.faulty(&pgfault.Tracer{Query: "SELECT id, channel_id, events FROM outgoing_webhooks WHERE enabled"})
	w := f.json(h, "POST", "/admin/channels/"+f.ch.String()+"/outgoing-webhooks", `{"name":"n","url":"https://hooks.example/y","events":["MESSAGE_CREATE"]}`)
	wantStatus(t, w, 201, "create")
}

func TestOutgoingManagementEdges(t *testing.T) {
	f := newFixture(t)
	rec := f.outgoing("https://hooks.example/x")
	base := "/admin/outgoing-webhooks/" + rec.ID.String()

	// Without the permission model every route is open; with a model that
	// cannot be loaded they fail closed.
	f.h.Perms = perm.NewService(nil)
	f.owner = &auth.User{ID: uuid.New(), Role: "user"}
	wantStatus(t, f.serve(f.h, "GET", "/admin/channels/"+f.ch.String()+"/outgoing-webhooks", nil, nil), 500, "list closed")
	wantStatus(t, f.json(f.h, "POST", "/admin/channels/"+f.ch.String()+"/outgoing-webhooks", `{}`), 500, "create closed")
	wantStatus(t, f.json(f.h, "PATCH", base, `{}`), 500, "update closed")
	f.h.Perms = nil

	// Bad bodies and ids.
	f.owner = &auth.User{ID: uuid.New(), Role: "admin"}
	wantStatus(t, f.json(f.h, "POST", "/admin/channels/zzz/outgoing-webhooks", `{}`), 400, "bad channel id")
	wantStatus(t, f.serve(f.h, "GET", "/admin/channels/zzz/outgoing-webhooks", nil, nil), 400, "bad channel id list")
	wantStatus(t, f.json(f.h, "POST", "/admin/channels/"+f.ch.String()+"/outgoing-webhooks", `{"nope":1}`), 400, "unknown field")
	// Updating nothing is fine.
	wantStatus(t, f.json(f.h, "PATCH", base, `{}`), 200, "empty patch")
}

func TestProcessDeliversAndSettles(t *testing.T) {
	f := newFixture(t)
	var hits atomic.Int32
	var retryAfter atomic.Value
	retryAfter.Store("")
	recv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		if v := retryAfter.Load().(string); v != "" {
			w.Header().Set("Retry-After", v)
		}
		status := http.StatusInternalServerError
		if r.URL.Path == "/ok" {
			status = http.StatusNoContent
		}
		if r.URL.Path == "/gone" {
			status = http.StatusGone
		}
		w.WriteHeader(status)
	}))
	defer recv.Close()
	d := NewDispatcher(f.p, loopbackClient(), "test")
	d.SetBackoff([]time.Duration{time.Millisecond})
	ctx := context.Background()

	// Success.
	ok := f.outgoing(recv.URL + "/ok")
	d.process(ctx, job{webhook: ok.ID, event: sampleEvent(), attempt: 1})
	cur, _ := getOutgoing(ctx, f.p, ok.ID)
	if cur.LastSuccessAt == nil || cur.ConsecutiveFailures != 0 {
		t.Fatalf("success state %+v", cur.Outgoing)
	}
	// A webhook that is gone or switched off is skipped quietly.
	d.process(ctx, job{webhook: uuid.New(), event: sampleEvent(), attempt: 1})
	if err := updateOutgoing(ctx, f.p, ok.ID, outgoingChange{enabled: ptr(false)}); err != nil {
		t.Fatal(err)
	}
	before := hits.Load()
	d.process(ctx, job{webhook: ok.ID, event: sampleEvent(), attempt: 1})
	if hits.Load() != before {
		t.Fatal("a switched-off webhook was called")
	}

	// A failure with attempts left schedules the retry and honours Retry-After.
	bad := f.outgoing(recv.URL + "/fail")
	d.process(ctx, job{webhook: bad.ID, event: sampleEvent(), attempt: 1})
	select {
	case j := <-d.queue:
		if j.attempt != 2 || j.webhook != bad.ID {
			t.Fatalf("retry job %+v", j)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no retry was queued")
	}
	retryAfter.Store("1")
	d.process(ctx, job{webhook: bad.ID, event: sampleEvent(), attempt: 1})
	select {
	case <-d.queue:
		t.Fatal("Retry-After was not honoured")
	case <-time.After(300 * time.Millisecond):
	}
	retryAfter.Store("")

	// Out of attempts: the failure counts, 410 does not retry at all.
	d.process(ctx, job{webhook: bad.ID, event: sampleEvent(), attempt: 2})
	if cur, _ := getOutgoing(ctx, f.p, bad.ID); cur.ConsecutiveFailures != 1 {
		t.Fatalf("failure not counted: %+v", cur.Outgoing)
	}
	// Too many retries pending: the event is given up instead of waiting.
	d.retrying.Store(maxRetrying)
	d.process(ctx, job{webhook: bad.ID, event: sampleEvent(), attempt: 1})
	d.retrying.Store(0)
	if cur, _ := getOutgoing(ctx, f.p, bad.ID); cur.ConsecutiveFailures != 2 {
		t.Fatalf("failure not counted: %+v", cur.Outgoing)
	}

	// Recording the attempt fails: nothing else happens.
	d2 := NewDispatcher(dbFaulty{f, "INSERT INTO outgoing_deliveries"}.pool(), loopbackClient(), "test")
	d2.process(ctx, job{webhook: bad.ID, event: sampleEvent(), attempt: 1})
	// A failing reload after a final failure is logged only.
	gone := f.outgoing(recv.URL + "/gone")
	d3 := NewDispatcher(dbFaulty{f, "SELECT id, channel_id, events FROM outgoing_webhooks WHERE enabled"}.pool(), loopbackClient(), "test")
	d3.process(ctx, job{webhook: gone.ID, event: sampleEvent(), attempt: 1})
}

func ptr[T any](v T) *T { return &v }

func TestRunWorksTheQueue(t *testing.T) {
	f := newFixture(t)
	got := make(chan struct{}, 4)
	recv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		got <- struct{}{}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer recv.Close()
	rec := f.outgoing(recv.URL)

	// A pool whose list query fails shows the load and reload errors; the
	// queue is still worked.
	d := NewDispatcher((&dbFaulty{f, "SELECT id, channel_id, events FROM outgoing_webhooks WHERE enabled"}).pool(), loopbackClient(), "test")
	d.ReloadEvery = 10 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { d.Run(ctx); close(done) }()
	d.enqueue(job{webhook: rec.ID, event: sampleEvent(), attempt: 1})
	select {
	case <-got:
	case <-time.After(5 * time.Second):
		t.Fatal("queued event was not delivered")
	}
	time.Sleep(50 * time.Millisecond) // a few reload ticks
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not stop")
	}
	// A full queue drops events instead of blocking.
	full := NewDispatcher(f.p, loopbackClient(), "test")
	for range queueSize + 205 {
		full.enqueue(job{webhook: rec.ID, event: sampleEvent(), attempt: 1})
	}
	if full.dropped.Load() != 205 {
		t.Fatalf("dropped %d", full.dropped.Load())
	}
}

func TestReloadErrors(t *testing.T) {
	f := newFixture(t)
	f.outgoing("https://hooks.example/x")
	for _, tr := range []*pgfault.Tracer{
		{Query: "SELECT id, channel_id, events FROM outgoing_webhooks WHERE enabled"},
		{Query: "SELECT id, channel_id, events FROM outgoing_webhooks WHERE enabled", AfterPrepare: true},
	} {
		d := NewDispatcher((&dbFaulty{f, tr.Query}).poolWith(tr), nil, "t")
		if err := d.Reload(context.Background()); err == nil {
			t.Errorf("reload with %+v succeeded", tr)
		}
	}
	d := NewDispatcher(f.p, nil, "t")
	if err := d.Reload(context.Background()); err != nil || len(*d.routes.Load()) != 1 {
		t.Fatalf("reload: %v %v", err, *d.routes.Load())
	}
}

// dbFaulty builds dispatchers on a pool that fails one statement.
type dbFaulty struct {
	f     *fixture
	query string
}

func (d dbFaulty) pool() *db.Pool { return d.poolWith(&pgfault.Tracer{Query: d.query}) }

func (d dbFaulty) poolWith(tr *pgfault.Tracer) *db.Pool {
	return &db.Pool{Pool: pgfault.Pool(d.f.t, d.f.p.Pool, tr)}
}
