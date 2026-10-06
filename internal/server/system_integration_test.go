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
	files, _ := db.MigrationFiles()
	h := st.Health
	if !h.Database.Reachable || h.Database.AppliedMigrations != len(files) || h.Database.PendingMigrations != 0 ||
		h.Database.LatestMigration != files[len(files)-1] {
		t.Fatalf("database = %+v (want %d migrations, latest %s)", h.Database, len(files), files[len(files)-1])
	}
	if !h.Storage.Configured || !h.Storage.Reachable || h.Storage.Files != 1 || h.Storage.TotalBytes <= 0 ||
		h.Storage.AttachmentBytes != h.Storage.TotalBytes || h.Storage.AvatarBytes != 0 {
		t.Fatalf("storage = %+v", h.Storage)
	}
	if h.Voice.Enabled || !h.Voice.TURNConfigured || h.Voice.STUNConfigured || h.Voice.WebSocketConnections != 1 || h.Voice.OnlineUsers != 1 {
		t.Fatalf("voice = %+v", h.Voice)
	}
	if h.Runtime.Goroutines <= 0 || h.Runtime.MemAllocBytes == 0 || h.Runtime.StartedAt.IsZero() {
		t.Fatalf("runtime = %+v", h.Runtime)
	}
	body := string(res.body)
	for _, secret := range []string{turnSecret, "integration-test-secret", "postgres://", "turn.example.com"} {
		if strings.Contains(body, secret) {
			t.Fatalf("system status leaks %q: %s", secret, body)
		}
	}
}
