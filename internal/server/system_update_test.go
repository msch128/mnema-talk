package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/msch128/mnema-talk/internal/update"
)

// fakeClock is a settable time source for the update checker.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) set(t time.Time) { c.mu.Lock(); c.t = t; c.mu.Unlock() }

func (c *fakeClock) Add(d time.Duration) { c.mu.Lock(); c.t = c.t.Add(d); c.mu.Unlock() }

func (c *fakeClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }

func TestUpdateBackoffIsReportedAndEnforced(t *testing.T) {
	var calls atomic.Int32
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(http.StatusTooManyRequests) }))
	defer remote.Close()
	checker := update.NewChecker(remote.URL, "0.3.0")
	var clock fakeClock
	clock.set(time.Now())
	checker.SetClock(clock.Now)
	if err := checker.Check(context.Background()); err == nil {
		t.Fatal("rate limit was accepted")
	}
	h := &systemHandler{updates: checker, version: "0.3.0"}
	status := h.updateStatus()
	if status.RetryAt == nil || status.CheckError == "" {
		t.Fatalf("status=%+v", status)
	}
	rec := httptest.NewRecorder()
	if err := h.checkNow(rec, httptest.NewRequest(http.MethodPost, "/", nil)); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" || calls.Load() != 1 {
		t.Fatalf("response=%d calls=%d", rec.Code, calls.Load())
	}
	// The one-minute manual cooldown wins immediately after an attempt. Once
	// it elapses the longer GitHub backoff must still prevent another request.
	clock.Add(update.ManualMinGap + time.Second)
	rec = httptest.NewRecorder()
	if err := h.checkNow(rec, httptest.NewRequest(http.MethodPost, "/", nil)); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "60" || calls.Load() != 1 {
		t.Fatalf("backoff response=%d retry=%s calls=%d", rec.Code, rec.Header().Get("Retry-After"), calls.Load())
	}
}
