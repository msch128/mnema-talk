package updaterd

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/msch128/mnema-talk/internal/imageverify"
)

// brokenDaemon answers every Engine API call with the given status.
func brokenDaemon(t *testing.T, status int) *Docker {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "upd")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := filepath.Join(dir, "d.sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Skipf("no unix sockets here: %v", err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = io.WriteString(w, `{"message":"`+strings.Repeat("boom ", 100)+`"}`)
	})}
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(func() { _ = srv.Close() })
	return NewDocker(sock)
}

func TestDockerClientReportsEngineErrors(t *testing.T) {
	d := brokenDaemon(t, http.StatusInternalServerError)
	ctx := context.Background()
	if _, err := d.Targets(ctx, "s"); err == nil || !strings.Contains(err.Error(), "HTTP 500") || len(err.Error()) > 300 {
		t.Errorf("Targets: %v", err)
	}
	if _, err := d.configImage(ctx, "abc"); err == nil {
		t.Error("inspect error not reported")
	}
	if err := d.Pull(ctx, "r/x", "sha256:1"); err == nil {
		t.Error("pull error not reported")
	}
	if err := d.Tag(ctx, "r/x", "sha256:1", "1.0"); err == nil {
		t.Error("tag error not reported")
	}
	if err := d.Tag(ctx, "", "", ""); err == nil {
		t.Error("tag without arguments must be refused")
	}

	gone := NewDocker("/nonexistent/docker.sock")
	if _, err := gone.Targets(ctx, "s"); err == nil {
		t.Error("an unreachable daemon must be an error")
	}
	if err := gone.Pull(ctx, "r/x", "sha256:1"); err == nil {
		t.Error("an unreachable daemon must be an error")
	}
	if err := gone.Tag(ctx, "r/x", "sha256:1", "1.0"); err == nil {
		t.Error("an unreachable daemon must be an error")
	}
	if _, err := gone.configImage(ctx, "abc"); err == nil {
		t.Error("an unreachable daemon must be an error")
	}
}

func TestRecreatorErrors(t *testing.T) {
	ctx := context.Background()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusUnauthorized) }))
	defer srv.Close()
	r := &HTTPRecreator{Endpoint: srv.URL, Token: "t", Client: srv.Client()}
	if err := r.Recreate(ctx); err == nil || !strings.Contains(err.Error(), "401") {
		t.Errorf("err = %v", err)
	}
	srv.Close()
	if err := r.Recreate(ctx); err == nil {
		t.Error("an unreachable recreator must be an error")
	}
	if err := (&HTTPRecreator{Endpoint: "://bad", Client: http.DefaultClient}).Recreate(ctx); err == nil {
		t.Error("a malformed endpoint must be an error")
	}
}

type recordingEngine struct {
	stubEngine
	calls    []string
	tagError error
}

func (e *recordingEngine) Pull(_ context.Context, repo, digest string) error {
	e.calls = append(e.calls, "pull "+repo+"@"+digest)
	return nil
}

func (e *recordingEngine) Tag(_ context.Context, _, _, tag string) error {
	e.calls = append(e.calls, "tag "+tag)
	return e.tagError
}

type okVerifier struct{}

func (okVerifier) Verify(_ context.Context, image string) (*imageverify.Result, error) {
	return &imageverify.Result{Repository: "r/x", Digest: "sha256:" + strings.Repeat("a", 64)}, nil
}

type countingRecreator struct{ n int }

func (c *countingRecreator) Recreate(context.Context) error { c.n++; return nil }

func TestSwapDoesNotRestartWhenTaggingFails(t *testing.T) {
	eng := &recordingEngine{stubEngine: stubEngine{targets: []Target{{Name: "app", Image: "r/x:1"}}}, tagError: context.DeadlineExceeded}
	rec := &countingRecreator{}
	srv := &Server{
		Token: token, Engine: eng, Verifier: okVerifier{}, Recreator: rec,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Background: func(f func()) { f() },
	}
	if r := post(srv, "Bearer "+token); r.Code != http.StatusAccepted {
		t.Fatalf("status = %d", r.Code)
	}
	if rec.n != 0 {
		t.Fatal("a failed tag must not lead to a restart")
	}
	if len(eng.calls) != 2 {
		t.Fatalf("calls = %v", eng.calls)
	}
	// And the slot is free again afterwards.
	if r := post(srv, "Bearer "+token); r.Code != http.StatusAccepted {
		t.Fatalf("second attempt: status = %d", r.Code)
	}
}

func TestSwapRunsInTheBackgroundByDefault(t *testing.T) {
	eng := &recordingEngine{stubEngine: stubEngine{targets: []Target{{Name: "app", Image: "r/x:1"}}}}
	done := make(chan struct{})
	rec := &signalRecreator{done: done}
	srv := &Server{Token: token, Engine: eng, Verifier: okVerifier{}, Recreator: rec, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if r := post(srv, "Bearer "+token); r.Code != http.StatusAccepted {
		t.Fatalf("status = %d", r.Code)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the swap never ran")
	}
}

type signalRecreator struct{ done chan struct{} }

func (s *signalRecreator) Recreate(context.Context) error { close(s.done); return nil }

func TestRunServesAndStopsOnSignal(t *testing.T) {
	env := map[string]string{"UPDATER_TOKEN": token, "UPDATER_LISTEN": "127.0.0.1:0"}
	done := make(chan int, 1)
	go func() { done <- Run(func(k string) string { return env[k] }, io.Discard) }()
	time.Sleep(300 * time.Millisecond)
	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("exit code = %d", code)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not stop")
	}
}

func TestRunRefusesAnOccupiedPort(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip(err)
	}
	defer l.Close()
	env := map[string]string{"UPDATER_TOKEN": token, "UPDATER_LISTEN": l.Addr().String()}
	if code := Run(func(k string) string { return env[k] }, io.Discard); code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
}
