//go:build integration

package webhook

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/perm/permtest"
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
	return f.outgoingFor(url, EventMessageCreate)
}

// outgoingFor creates a webhook of the fixture's channel for the events.
func (f *fixture) outgoingFor(url string, events ...string) *outgoingRec {
	f.t.Helper()
	rec, _, err := createOutgoing(context.Background(), f.p, f.ch, f.owner.ID, "hook", url, events)
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
	h := f.faulty(&pgfault.Tracer{Query: "SELECT w.id, w.channel_id, w.events FROM outgoing_webhooks w"})
	w := f.json(h, "POST", "/admin/channels/"+f.ch.String()+"/outgoing-webhooks", `{"name":"n","url":"https://hooks.example/y","events":["MESSAGE_CREATE"]}`)
	wantStatus(t, w, 201, "create")
}

func TestOutgoingManagementEdges(t *testing.T) {
	f := newFixture(t)
	rec := f.outgoing("https://hooks.example/x")
	base := "/admin/outgoing-webhooks/" + rec.ID.String()

	// With a permission model that cannot be loaded every route fails closed.
	perms := f.h.Perms
	f.h.Perms = permtest.Unloadable()
	f.owner = &auth.User{ID: uuid.New(), Role: "user"}
	wantStatus(t, f.serve(f.h, "GET", "/admin/channels/"+f.ch.String()+"/outgoing-webhooks", nil, nil), 500, "list closed")
	wantStatus(t, f.json(f.h, "POST", "/admin/channels/"+f.ch.String()+"/outgoing-webhooks", `{}`), 500, "create closed")
	wantStatus(t, f.json(f.h, "PATCH", base, `{}`), 500, "update closed")
	f.h.Perms = perms

	// Bad bodies and ids.
	f.owner = &auth.User{ID: uuid.New(), Role: "admin"}
	wantStatus(t, f.json(f.h, "POST", "/admin/channels/zzz/outgoing-webhooks", `{}`), 400, "bad channel id")
	wantStatus(t, f.serve(f.h, "GET", "/admin/channels/zzz/outgoing-webhooks", nil, nil), 400, "bad channel id list")
	wantStatus(t, f.json(f.h, "POST", "/admin/channels/"+f.ch.String()+"/outgoing-webhooks", `{"nope":1}`), 400, "unknown field")
	// Updating nothing is fine.
	wantStatus(t, f.json(f.h, "PATCH", base, `{}`), 200, "empty patch")
}

func TestReloadErrors(t *testing.T) {
	f := newFixture(t)
	f.outgoing("https://hooks.example/x")
	for _, tr := range []*pgfault.Tracer{
		{Query: "SELECT w.id, w.channel_id, w.events FROM outgoing_webhooks w"},
		{Query: "SELECT w.id, w.channel_id, w.events FROM outgoing_webhooks w", AfterPrepare: true},
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

func TestLateFailureDoesNotSwitchOffAnAdminDisabledWebhook(t *testing.T) {
	f := newFixture(t)
	hook := f.outgoing("https://hooks.example/x")
	ctx := context.Background()
	fail := Delivery{EventID: uuid.New(), EventType: EventMessageCreate, Attempt: 1}
	if _, err := f.p.Exec(ctx, `UPDATE outgoing_webhooks SET consecutive_failures = $2, enabled = FALSE WHERE id = $1`, hook.ID, disableAfter-1); err != nil {
		t.Fatal(err)
	}
	off, err := recordDelivery(ctx, f.p, hook.ID, fail, true, true)
	if err != nil || off {
		t.Fatalf("admin-disabled webhook: switchedOff=%v err=%v", off, err)
	}
	var auto bool
	if err := f.p.QueryRow(ctx, `SELECT auto_disabled FROM outgoing_webhooks WHERE id = $1`, hook.ID).Scan(&auto); err != nil || auto {
		t.Fatalf("auto_disabled=%v err=%v", auto, err)
	}

	// An enabled one at the threshold switches off exactly once.
	if _, err := f.p.Exec(ctx, `UPDATE outgoing_webhooks SET enabled = TRUE WHERE id = $1`, hook.ID); err != nil {
		t.Fatal(err)
	}
	if off, err = recordDelivery(ctx, f.p, hook.ID, fail, true, true); err != nil || !off {
		t.Fatalf("first failure at the threshold: switchedOff=%v err=%v", off, err)
	}
	if off, err = recordDelivery(ctx, f.p, hook.ID, fail, true, true); err != nil || off {
		t.Fatalf("late failure of an auto-disabled webhook: switchedOff=%v err=%v", off, err)
	}
}
