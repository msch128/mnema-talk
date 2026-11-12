package updaterd

import (
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"
)

// WaitIdle (shutdown) waits on a sync.WaitGroup while a handler that outlived
// http.Server.Shutdown can still ask for a swap. From the start of WaitIdle on
// no swap is admitted: it would start while the process exits, and an Add
// concurrent with the Wait is a WaitGroup misuse (H-L7-08).
func TestNoSwapIsAdmittedAfterWaitIdleReturned(t *testing.T) {
	s := &Server{}
	if !s.WaitIdle(time.Second) {
		t.Fatal("an idle updater must report idle at once")
	}
	if s.admit() {
		t.Fatal("a swap was admitted after WaitIdle")
	}
	if !s.WaitIdle(time.Second) {
		t.Fatal("the updater must still be idle")
	}
}

func TestUpdateRequestAfterWaitIdleIsRefusedWith503(t *testing.T) {
	eng := &recordingEngine{stubEngine: stubEngine{targets: []Target{{Name: "app", Image: "r/x:1", RunningVersion: "1.0.0"}}}}
	rec := &countingRecreator{}
	srv := &Server{
		Token: token, Engine: eng, Verifier: okVerifier{}, Recreator: rec,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Background: func(f func()) { f() },
	}
	if !srv.WaitIdle(time.Second) {
		t.Fatal("an idle updater must report idle at once")
	}
	// The second request shows the refusal released the busy flag (else 429).
	for range 2 {
		if r := post(srv, "Bearer "+token); r.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503", r.Code)
		}
	}
	if len(eng.calls) != 0 || rec.n != 0 {
		t.Fatalf("a refused update touched the engine: calls %v, recreated %d", eng.calls, rec.n)
	}
}
