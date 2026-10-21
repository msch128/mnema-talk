//go:build integration

package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/msch128/mnema-talk/internal/update"
)

// fakeGitHub serves one release and counts requests.
func fakeGitHub(t *testing.T, tag string) (*update.Checker, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`{"tag_name":"` + tag + `","html_url":"https://github.com/msch128/mnema-talk/releases/tag/` + tag + `","published_at":"2026-10-05T12:00:00Z","body":"* fix"}`)); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	t.Cleanup(srv.Close)
	return update.NewChecker(srv.URL, "0.3.0"), &hits
}

func assertCheckUnauthorizedOrForbidden(t *testing.T, a *app, admin, max *client) {
	t.Helper()
	if res := max.post("/api/admin/system/check", nil); res.status != http.StatusForbidden {
		t.Fatalf("member check: %d", res.status)
	}
	if res := a.anon().post("/api/admin/system/check", nil); res.status != http.StatusUnauthorized {
		t.Fatalf("anonymous check: %d", res.status)
	}
	if res := admin.do(http.MethodPost, "/api/admin/system/check", nil, map[string]string{"Origin": "https://evil.example"}); res.status != http.StatusForbidden {
		t.Fatalf("cross-origin check: %d", res.status)
	}
}

func assertUpdateStatusAvailable(t *testing.T, st UpdateStatus) {
	t.Helper()
	if !st.UpdateAvailable {
		t.Fatalf("expected update to be available: %+v", st)
	}
	if st.LatestVersion != "0.4.0" || st.CurrentVersion != "0.3.0" {
		t.Fatalf("unexpected versions: latest %s, current %s", st.LatestVersion, st.CurrentVersion)
	}
	if st.ReleaseNotes != "* fix" || st.ReleaseURL != "https://github.com/msch128/mnema-talk/releases/tag/v0.4.0" {
		t.Fatalf("unexpected release metadata: notes %s, url %s", st.ReleaseNotes, st.ReleaseURL)
	}
	if st.CheckedAt == nil {
		t.Fatal("expected checkedAt to be set")
	}
}

func TestUpdateCheckReportsANewRelease(t *testing.T) {
	checker, hits := fakeGitHub(t, "v0.4.0")
	a := newAppWithDeps(t, false, nil, func(d *Deps) { d.Version = "0.3.0"; d.Updates = checker })
	admin := a.seedAdmin()
	max := a.register(admin, "max")

	// Nothing is fetched until a check runs.
	var st UpdateStatus
	res := admin.get("/api/admin/system/update")
	res.decode(t, &st)
	if res.status != http.StatusOK {
		t.Fatalf("before check status: %d", res.status)
	}
	if !st.CheckEnabled || st.LatestVersion != "" || st.UpdateAvailable {
		t.Fatalf("before check update status: %+v", st)
	}
	if hits.Load() != 0 {
		t.Fatalf("before check hits = %d", hits.Load())
	}

	assertCheckUnauthorizedOrForbidden(t, a, admin, max)
	if hits.Load() != 0 {
		t.Fatalf("rejected requests reached GitHub: %d", hits.Load())
	}

	res = admin.post("/api/admin/system/check", nil)
	if res.status != http.StatusOK {
		t.Fatalf("check: %d %s", res.status, res.body)
	}
	res.decode(t, &st)
	assertUpdateStatusAvailable(t, st)

	// A second manual check within a minute is refused without a request.
	if res := admin.post("/api/admin/system/check", nil); res.status != http.StatusTooManyRequests || res.header.Get("Retry-After") == "" {
		t.Fatalf("second check: %d", res.status)
	}
	if hits.Load() != 1 {
		t.Fatalf("hits = %d", hits.Load())
	}

	var sys SystemStatus
	admin.get("/api/admin/system").decode(t, &sys)
	if !sys.Update.UpdateAvailable || sys.Update.LatestVersion != "0.4.0" {
		t.Fatalf("system status update = %+v", sys.Update)
	}
}

func TestUpdateCheckSameVersionIsNoUpdate(t *testing.T) {
	checker, hits := fakeGitHub(t, "v0.3.0")
	if err := checker.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 1 {
		t.Fatalf("expected 1 hit, got %d", hits.Load())
	}
	a := newAppWithDeps(t, false, nil, func(d *Deps) { d.Version = "0.3.0"; d.Updates = checker })
	admin := a.seedAdmin()
	var st UpdateStatus
	admin.get("/api/admin/system/update").decode(t, &st)
	if st.UpdateAvailable || st.LatestVersion != "0.3.0" {
		t.Fatalf("status = %+v", st)
	}
}

func TestUpdateCheckDisabled(t *testing.T) {
	a := newAppWithDeps(t, false, nil, func(d *Deps) { d.Version = "0.3.0" })
	admin := a.seedAdmin()
	var st UpdateStatus
	admin.get("/api/admin/system/update").decode(t, &st)
	if st.CheckEnabled || st.LatestVersion != "" {
		t.Fatalf("status = %+v", st)
	}
	if res := admin.post("/api/admin/system/check", nil); res.status != http.StatusConflict {
		t.Fatalf("check while disabled: %d %s", res.status, res.body)
	}
}
