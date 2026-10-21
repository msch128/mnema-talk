//go:build integration

package server

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/msch128/mnema-talk/internal/config"
	"github.com/msch128/mnema-talk/internal/db"
)

func TestSystemStatusIsAdminOnly(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	max := a.register(admin, "max")

	if res := a.anon().get("/api/admin/system"); res.status != http.StatusUnauthorized {
		t.Fatalf("anonymous: %d %s", res.status, res.body)
	}
	if res := max.get("/api/admin/system"); res.status != http.StatusForbidden {
		t.Fatalf("member: %d %s", res.status, res.body)
	}
	if res := admin.get("/api/admin/system"); res.status != http.StatusOK {
		t.Fatalf("admin: %d %s", res.status, res.body)
	}
}

func assertDatabaseHealth(t *testing.T, dbHealth DatabaseHealth) {
	t.Helper()
	files, err := db.MigrationFiles()
	if err != nil {
		t.Fatalf("migration files: %v", err)
	}
	if !dbHealth.Reachable {
		t.Fatalf("database unreachable: %+v", dbHealth)
	}
	if dbHealth.AppliedMigrations != len(files) || dbHealth.PendingMigrations != 0 {
		t.Fatalf("database migrations count mismatch: %+v (want %d)", dbHealth, len(files))
	}
	if dbHealth.LatestMigration != files[len(files)-1] {
		t.Fatalf("database latest migration mismatch: got %q, want %q", dbHealth.LatestMigration, files[len(files)-1])
	}
}

func assertStorageHealth(t *testing.T, s StorageHealth) {
	t.Helper()
	if !s.Configured || !s.Reachable {
		t.Fatalf("storage unreachable or unconfigured: %+v", s)
	}
	if s.Files != 1 || s.TotalBytes <= 0 {
		t.Fatalf("unexpected storage files or bytes: %+v", s)
	}
	if s.AttachmentBytes != s.TotalBytes || s.AvatarBytes != 0 {
		t.Fatalf("unexpected storage category bytes: %+v", s)
	}
}

func assertVoiceHealth(t *testing.T, v VoiceHealth) {
	t.Helper()
	if v.Enabled || v.STUNConfigured {
		t.Fatalf("unexpected voice enable/stun flags: %+v", v)
	}
	if !v.TURNConfigured {
		t.Fatalf("expected turn configured: %+v", v)
	}
	if v.WebSocketConnections != 1 || v.OnlineUsers != 1 {
		t.Fatalf("unexpected connection counts: %+v", v)
	}
}

func assertRuntimeHealth(t *testing.T, r RuntimeHealth) {
	t.Helper()
	if r.Goroutines <= 0 || r.MemAllocBytes == 0 || r.StartedAt.IsZero() {
		t.Fatalf("runtime health invalid: %+v", r)
	}
}

func TestSystemStatusReportsHealthWithoutSecrets(t *testing.T) {
	turnSecret := strings.Repeat("turn-", 5)
	a := newAppWithDeps(t, true, func(c *config.Config) {
		c.WebRTCTURNURLs = []string{"turn:turn.example.com:3478"}
		c.WebRTCTURNSecret = turnSecret
	}, func(d *Deps) { d.Version = "0.4.0" })
	admin := a.seedAdmin()
	ch := a.createChannel(admin, "general", "text")
	if res := admin.upload("/api/channels/"+ch.String()+"/upload", "file", "a.png", "image/png", pngBytes, nil); res.status != http.StatusCreated {
		t.Fatalf("upload: %d %s", res.status, res.body)
	}
	conn, _, err := admin.dialWS(a.origin())
	if err != nil {
		t.Fatal(err)
	}
	// Sent from register: the hub counts the connection from here on.
	if _, ok := conn.expect("server_info", 2*time.Second); !ok {
		t.Fatal("no server_info")
	}

	res := admin.get("/api/admin/system")
	if res.status != http.StatusOK {
		t.Fatalf("status: %d %s", res.status, res.body)
	}
	if res.header.Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q", res.header.Get("Cache-Control"))
	}
	var st SystemStatus
	res.decode(t, &st)

	if st.Version.Current != "0.4.0" || !strings.HasPrefix(st.Version.GoVersion, "go") {
		t.Fatalf("version = %+v", st.Version)
	}
	assertDatabaseHealth(t, st.Health.Database)
	assertStorageHealth(t, st.Health.Storage)
	assertVoiceHealth(t, st.Health.Voice)
	assertRuntimeHealth(t, st.Health.Runtime)
	body := string(res.body)
	for _, secret := range []string{turnSecret, "integration-test-secret", "postgres://", "turn.example.com"} {
		if strings.Contains(body, secret) {
			t.Fatalf("system status leaks %q: %s", secret, body)
		}
	}
}
