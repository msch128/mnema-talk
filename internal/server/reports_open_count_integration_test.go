//go:build integration

package server

import (
	"net/http"
	"testing"
	"time"

	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/perm"
)

func openCountOf(t *testing.T, w *wsConn) (int, bool) {
	t.Helper()
	raw, ok := w.expect(events.ReportsOpen, 3*time.Second)
	if !ok {
		return 0, false
	}
	return decodeEvent[struct {
		Open int `json:"open"`
	}](t, raw).Open, true
}

// newLiveReportsFixture is newReportsFixture on an app whose events reach real sockets.
func newLiveReportsFixture(t *testing.T) *reportsFixture {
	t.Helper()
	a := newApp(t, true)
	admin := a.seedAdmin()
	f := &reportsFixture{a: a, admin: admin}
	f.reporter = a.register(admin, "reporter")
	f.author = a.register(admin, "author")
	f.moderator = a.register(admin, "moderator")
	assignRoles(t, admin, f.moderator, createRole(t, admin, "report readers", perm.ManageReports))
	f.channel = a.createChannel(admin, "general", "text")
	return f
}

func TestModeratorsSeeTheOpenReportCountLiveAndNobodyElseDoes(t *testing.T) {
	f := newLiveReportsFixture(t)
	moderator := f.moderator.dialLiveWS(f.a.origin())
	owner := f.admin.dialLiveWS(f.a.origin())
	reporter := f.reporter.dialLiveWS(f.a.origin())
	author := f.author.dialLiveWS(f.a.origin())

	id := f.file(f.a.send(f.author, f.channel, "rude"))
	f.file(f.a.send(f.author, f.channel, "ruder"))
	for name, w := range map[string]*wsConn{"moderator": moderator, "owner": owner} {
		if n, ok := openCountOf(t, w); !ok || n != 1 {
			t.Fatalf("%s after the first report: count %d, received %v", name, n, ok)
		}
		if n, ok := openCountOf(t, w); !ok || n != 2 {
			t.Fatalf("%s after the second report: count %d, received %v", name, n, ok)
		}
	}

	wantStatus(t, "resolve", f.moderator.post("/api/admin/reports/"+id.String()+"/resolve", map[string]any{"status": "dismissed"}), http.StatusOK)
	if n, ok := openCountOf(t, moderator); !ok || n != 1 {
		t.Fatalf("after dismissing one: count %d, received %v", n, ok)
	}

	// Members without Manage reports never learn how many reports are open.
	for name, w := range map[string]*wsConn{"reporter": reporter, "author": author} {
		if got := w.collect(time.Second, events.ReportsOpen); len(got) != 0 {
			t.Fatalf("%s received the open-report count: %s", name, got[events.ReportsOpen])
		}
	}
}

func TestTheOpenReportCountIsReadableByModeratorsOnly(t *testing.T) {
	f := newReportsFixture(t)
	f.file(f.a.send(f.author, f.channel, "rude"))

	res := f.moderator.get("/api/admin/reports/open-count")
	wantStatus(t, "moderator count", res, http.StatusOK)
	var body struct{ Open int }
	res.decode(t, &body)
	if body.Open != 1 {
		t.Fatalf("open = %d, want 1", body.Open)
	}
	wantStatus(t, "member count", f.reporter.get("/api/admin/reports/open-count"), http.StatusForbidden)
}
