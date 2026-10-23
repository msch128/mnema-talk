// Package daemontest is a fake Docker daemon (on a Unix socket) and a fake
// recreator for tests of the updater: both record what was done to them, in
// order, so a test can assert that a refused update touched nothing. Not for
// production code.
package daemontest

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// Daemon implements the part of the Engine API the updater calls.
type Daemon struct {
	// Socket is the path to dial.
	Socket string
	// Image is the Config.Image of the one container in scope.
	Image string
	// Version is the running container's image version label (default 1.0.1).
	Version string
	// PullFail, if set, is streamed back as the pull's error message.
	PullFail string

	mu    sync.Mutex
	calls []string
}

// Record appends one entry to the call log.
func (d *Daemon) Record(format string, args ...any) {
	d.mu.Lock()
	d.calls = append(d.calls, fmt.Sprintf(format, args...))
	d.mu.Unlock()
}

// Log returns the calls so far.
func (d *Daemon) Log() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.calls...)
}

// ServeHTTP is the fake Engine API.
func (d *Daemon) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/containers/json":
		var f map[string][]string
		_ = json.Unmarshal([]byte(r.URL.Query().Get("filters")), &f)
		d.Record("list labels=%s", strings.Join(f["label"], ","))
		_, _ = w.Write([]byte(`[{"Id":"abc123","Names":["/mnema-app"]}]`))
	case r.Method == http.MethodGet && r.URL.Path == "/containers/abc123/json":
		version := d.Version
		if version == "" {
			version = "1.0.1"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"Config": map[string]any{
			"Image": d.Image, "Labels": map[string]string{"org.opencontainers.image.version": version},
		}})
	case r.Method == http.MethodPost && r.URL.Path == "/images/create":
		q := r.URL.Query()
		d.Record("pull %s@%s", q.Get("fromImage"), q.Get("tag"))
		_, _ = io.WriteString(w, `{"status":"Pulling"}`+"\n")
		if d.PullFail != "" {
			_ = json.NewEncoder(w).Encode(map[string]string{"error": d.PullFail})
		}
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/tag"):
		q := r.URL.Query()
		d.Record("tag %s -> %s:%s", strings.TrimPrefix(strings.TrimSuffix(r.URL.Path, "/tag"), "/images/"), q.Get("repo"), q.Get("tag"))
		w.WriteHeader(http.StatusCreated)
	default:
		http.NotFound(w, r)
	}
}

// New starts a fake daemon whose one container runs image. The short socket
// path lives in /tmp because Unix socket paths are limited to about 100 bytes.
func New(t *testing.T, image string) *Daemon {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "upd")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	d := &Daemon{Socket: filepath.Join(dir, "d.sock"), Image: image}
	l, err := net.Listen("unix", d.Socket)
	if err != nil {
		t.Skipf("no unix sockets here: %v", err)
	}
	srv := &http.Server{Handler: d}
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(func() { _ = srv.Close() })
	return d
}

// RecreatorURL starts a fake recreator that logs "recreate <path> auth=<bool>"
// into the daemon's call log, and returns its update endpoint.
func (d *Daemon) RecreatorURL(t *testing.T, token string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		d.Record("recreate %s auth=%v", r.URL.Path, r.Header.Get("Authorization") == "Bearer "+token)
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/v1/update"
}
