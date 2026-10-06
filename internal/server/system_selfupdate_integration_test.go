//go:build integration

package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/msch128/mnema-talk/internal/config"
)

var updaterToken = strings.Repeat("updater-", 5)

// fakeSidecar stands in for the Watchtower HTTP API.
type fakeSidecar struct {
	srv    *httptest.Server
	calls  atomic.Int32
	status atomic.Int32
	auth   atomic.Value
}

func newFakeSidecar(t *testing.T) *fakeSidecar {
	t.Helper()
	f := &fakeSidecar{}
	f.status.Store(http.StatusAccepted)
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.calls.Add(1)
		f.auth.Store(r.Method + " " + r.URL.Path + " " + r.Header.Get("Authorization"))
		w.WriteHeader(int(f.status.Load()))
	}))
	t.Cleanup(f.srv.Close)
	return f
}

// selfUpdateApp: version 0.3.0, GitHub says 0.4.0 (checked), sidecar configured.
func selfUpdateApp(t *testing.T, sidecar *fakeSidecar, image string) *app {
	t.Helper()
	checker, _ := fakeGitHub(t, "v0.4.0")
	if err := checker.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	return newAppWithDeps(t, false, func(c *config.Config) {
		c.AppImage = image
		if sidecar != nil {
			c.UpdaterToken = updaterToken
			c.UpdaterURL = sidecar.srv.URL
		}
	}, func(d *Deps) { d.Version = "0.3.0"; d.Updates = checker })
}

func updateBody(password string) map[string]string {
	return map[string]string{"password": password, "target_version": "0.4.0"}
}

func TestSelfUpdateSuccess(t *testing.T) {
	sidecar := newFakeSidecar(t)
	a := selfUpdateApp(t, sidecar, "ghcr.io/msch128/mnema-talk:0.4")
	admin := a.seedAdmin()

	var st SystemStatus
	admin.get("/api/admin/system").decode(t, &st)
	if !st.SelfUpdate.Configured || !st.SelfUpdate.Available || st.SelfUpdate.Reach != "yes" || st.SelfUpdate.Image != "ghcr.io/msch128/mnema-talk:0.4" {
		t.Fatalf("self_update = %+v", st.SelfUpdate)
	}
	if strings.Contains(string(admin.get("/api/admin/system").body), updaterToken) {
		t.Fatal("system status leaks the updater token")
	}

	res := admin.post("/api/admin/system/update", updateBody("admin-password-123"))
	if res.status != http.StatusAccepted {
		t.Fatalf("update: %d %s", res.status, res.body)
	}
	var started SelfUpdateStarted
	res.decode(t, &started)
	if started.Status != "started" || started.FromVersion != "0.3.0" || started.TargetVersion != "0.4.0" {
		t.Fatalf("started = %+v", started)
	}
	if sidecar.calls.Load() != 1 || sidecar.auth.Load() != "POST /v1/update Bearer "+updaterToken {
		t.Fatalf("sidecar calls=%d last=%v", sidecar.calls.Load(), sidecar.auth.Load())
	}
	broadcast := false
	for _, ev := range a.events.Snapshot() {
		if ev.Type == "system_update" && ev.Recipients == nil {
			broadcast = true
		}
	}
	if !broadcast {
		t.Fatal("no system_update broadcast")
	}

	// One update per five minutes, even with the right password.
	res = admin.post("/api/admin/system/update", updateBody("admin-password-123"))
	if res.status != http.StatusTooManyRequests || res.header.Get("Retry-After") == "" {
		t.Fatalf("second update: %d %s", res.status, res.body)
	}
	admin.get("/api/admin/system").decode(t, &st)
	if st.SelfUpdate.Available || st.SelfUpdate.NextAllowedAt == nil {
		t.Fatalf("after update: %+v", st.SelfUpdate)
	}
	if sidecar.calls.Load() != 1 {
		t.Fatalf("sidecar called again: %d", sidecar.calls.Load())
	}
}

func TestSelfUpdateRequiresAdminSameOriginAndPassword(t *testing.T) {
	sidecar := newFakeSidecar(t)
	a := selfUpdateApp(t, sidecar, "ghcr.io/msch128/mnema-talk:latest")
	admin := a.seedAdmin()
	max := a.register(admin, "max")

	if res := a.anon().post("/api/admin/system/update", updateBody("admin-password-123")); res.status != http.StatusUnauthorized {
		t.Fatalf("anonymous: %d", res.status)
	}
	if res := max.post("/api/admin/system/update", updateBody("member-password-123")); res.status != http.StatusForbidden {
		t.Fatalf("member: %d", res.status)
	}
	if res := admin.do(http.MethodPost, "/api/admin/system/update", updateBody("admin-password-123"), map[string]string{"Origin": "https://evil.example"}); res.status != http.StatusForbidden {
		t.Fatalf("cross-origin: %d", res.status)
	}
	if res := admin.post("/api/admin/system/update", updateBody("wrong-password-1")); res.status != http.StatusForbidden {
		t.Fatalf("wrong password: %d %s", res.status, res.body)
	}
	if res := admin.post("/api/admin/system/update", map[string]string{"target_version": "0.4.0"}); res.status != http.StatusForbidden {
		t.Fatalf("no password: %d %s", res.status, res.body)
	}
	// Five wrong passwords lock the confirmation (shared with password changes).
	for i := 0; i < 3; i++ {
		admin.post("/api/admin/system/update", updateBody("wrong-password-1"))
	}
	if res := admin.post("/api/admin/system/update", updateBody("admin-password-123")); res.status != http.StatusTooManyRequests {
		t.Fatalf("after 5 wrong passwords: %d %s", res.status, res.body)
	}
	if res := admin.put("/api/auth/password", map[string]string{"current_password": "admin-password-123", "new_password": "another-password-1"}); res.status != http.StatusTooManyRequests {
		t.Fatalf("password change not locked as well: %d", res.status)
	}
	if sidecar.calls.Load() != 0 {
		t.Fatalf("sidecar called %d times", sidecar.calls.Load())
	}
}

func TestSelfUpdateRefusals(t *testing.T) {
	t.Run("not configured", func(t *testing.T) {
		a := selfUpdateApp(t, nil, "ghcr.io/msch128/mnema-talk:latest")
		admin := a.seedAdmin()
		var st SystemStatus
		admin.get("/api/admin/system").decode(t, &st)
		if st.SelfUpdate.Configured || st.SelfUpdate.Available {
			t.Fatalf("self_update = %+v", st.SelfUpdate)
		}
		if res := admin.post("/api/admin/system/update", updateBody("admin-password-123")); res.status != http.StatusConflict {
			t.Fatalf("update: %d %s", res.status, res.body)
		}
	})
	t.Run("stale target version", func(t *testing.T) {
		sidecar := newFakeSidecar(t)
		a := selfUpdateApp(t, sidecar, "ghcr.io/msch128/mnema-talk:latest")
		admin := a.seedAdmin()
		res := admin.post("/api/admin/system/update", map[string]string{"password": "admin-password-123", "target_version": "0.3.9"})
		if res.status != http.StatusConflict || sidecar.calls.Load() != 0 {
			t.Fatalf("update: %d %s calls=%d", res.status, res.body, sidecar.calls.Load())
		}
	})
	t.Run("pinned image", func(t *testing.T) {
		sidecar := newFakeSidecar(t)
		a := selfUpdateApp(t, sidecar, "ghcr.io/msch128/mnema-talk:0.3.0")
		admin := a.seedAdmin()
		var st SystemStatus
		admin.get("/api/admin/system").decode(t, &st)
		if st.SelfUpdate.Available || st.SelfUpdate.Reach != "no" || st.SelfUpdate.ReachReason != "version_pinned" {
			t.Fatalf("self_update = %+v", st.SelfUpdate)
		}
		if res := admin.post("/api/admin/system/update", updateBody("admin-password-123")); res.status != http.StatusConflict || sidecar.calls.Load() != 0 {
			t.Fatalf("update: %d calls=%d", res.status, sidecar.calls.Load())
		}
	})
	t.Run("no update available", func(t *testing.T) {
		sidecar := newFakeSidecar(t)
		checker, _ := fakeGitHub(t, "v0.3.0")
		_ = checker.Check(context.Background())
		a := newAppWithDeps(t, false, func(c *config.Config) {
			c.UpdaterToken, c.UpdaterURL = updaterToken, sidecar.srv.URL
		}, func(d *Deps) { d.Version = "0.3.0"; d.Updates = checker })
		admin := a.seedAdmin()
		res := admin.post("/api/admin/system/update", map[string]string{"password": "admin-password-123", "target_version": "0.3.0"})
		if res.status != http.StatusConflict || sidecar.calls.Load() != 0 {
			t.Fatalf("update: %d calls=%d", res.status, sidecar.calls.Load())
		}
	})
	t.Run("sidecar error", func(t *testing.T) {
		sidecar := newFakeSidecar(t)
		sidecar.status.Store(http.StatusUnauthorized)
		a := selfUpdateApp(t, sidecar, "ghcr.io/msch128/mnema-talk:latest")
		admin := a.seedAdmin()
		res := admin.post("/api/admin/system/update", updateBody("admin-password-123"))
		if res.status != http.StatusServiceUnavailable || !strings.Contains(string(res.body), "UPDATER_TOKEN") || strings.Contains(string(res.body), updaterToken) {
			t.Fatalf("update: %d %s", res.status, res.body)
		}
	})
	t.Run("sidecar busy", func(t *testing.T) {
		sidecar := newFakeSidecar(t)
		sidecar.status.Store(http.StatusTooManyRequests)
		a := selfUpdateApp(t, sidecar, "ghcr.io/msch128/mnema-talk:latest")
		admin := a.seedAdmin()
		if res := admin.post("/api/admin/system/update", updateBody("admin-password-123")); res.status != http.StatusConflict {
			t.Fatalf("update: %d %s", res.status, res.body)
		}
	})
	t.Run("sidecar down", func(t *testing.T) {
		sidecar := newFakeSidecar(t)
		a := selfUpdateApp(t, sidecar, "ghcr.io/msch128/mnema-talk:latest")
		sidecar.srv.Close()
		admin := a.seedAdmin()
		start := time.Now()
		res := admin.post("/api/admin/system/update", updateBody("admin-password-123"))
		if res.status != http.StatusServiceUnavailable || time.Since(start) > 20*time.Second {
			t.Fatalf("update: %d %s after %v", res.status, res.body, time.Since(start))
		}
	})
}
