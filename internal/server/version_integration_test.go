//go:build integration

package server

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// Every WebSocket connection learns the server's version first, so browsers
// can offer a reload after an update; /api/health tells anyone the version
// string and nothing else about the build.
func TestServerVersionIsAnnounced(t *testing.T) {
	a := newAppWithDeps(t, true, nil, func(d *Deps) { d.Version = "0.4.0" })
	admin := a.seedAdmin()

	conn, _, err := admin.dialWS(a.origin())
	if err != nil {
		t.Fatal(err)
	}
	raw, ok := conn.expect("server_info", 2*time.Second)
	if !ok {
		t.Fatal("no server_info event")
	}
	var info map[string]any
	if err := json.Unmarshal(raw, &info); err != nil {
		t.Fatal(err)
	}
	if info["version"] != "0.4.0" || len(info) != 1 {
		t.Fatalf("server_info = %v, want only version 0.4.0", info)
	}

	res := a.anon().get("/api/health")
	if res.status != http.StatusOK {
		t.Fatalf("health: %d %s", res.status, res.body)
	}
	var health map[string]any
	res.decode(t, &health)
	if health["version"] != "0.4.0" || len(health) != 2 {
		t.Fatalf("health = %v, want status and version only", health)
	}
}

func TestServerInfoWithoutVersionSaysDev(t *testing.T) {
	a := newApp(t, true)
	admin := a.seedAdmin()
	conn, _, err := admin.dialWS(a.origin())
	if err != nil {
		t.Fatal(err)
	}
	raw, ok := conn.expect("server_info", 2*time.Second)
	if !ok || string(raw) != `{"version":"dev"}` {
		t.Fatalf("server_info = %s (ok=%v)", raw, ok)
	}
}
