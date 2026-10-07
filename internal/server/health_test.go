package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/sfu"
	"github.com/msch128/mnema-talk/internal/ws"
)

func TestHealthCachesThePing(t *testing.T) {
	var pings atomic.Int32
	hc := &healthCheck{ttl: time.Hour, ping: func(context.Context) error {
		pings.Add(1)
		return nil
	}}
	h := health(hc, "1.2.3")

	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/health", nil))
			if rec.Code != http.StatusOK {
				t.Errorf("status %d", rec.Code)
			}
		}()
	}
	wg.Wait()
	if n := pings.Load(); n != 1 {
		t.Fatalf("database pinged %d times for 50 requests within the TTL, want 1", n)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	var body Health
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Status != "ok" || body.Version != "1.2.3" {
		t.Fatalf("body %s err %v", rec.Body, err)
	}
}

func TestHealthRechecksAfterTTL(t *testing.T) {
	var pings atomic.Int32
	fail := atomic.Bool{}
	fail.Store(true)
	hc := &healthCheck{ttl: 10 * time.Millisecond, ping: func(context.Context) error {
		pings.Add(1)
		if fail.Load() {
			return errors.New("down")
		}
		return nil
	}}
	h := health(hc, "")
	get := func() int {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/health", nil))
		return rec.Code
	}
	if c := get(); c != http.StatusServiceUnavailable {
		t.Fatalf("db down: status %d", c)
	}
	fail.Store(false)
	if c := get(); c != http.StatusServiceUnavailable {
		t.Fatalf("cached failure expected within the TTL, got %d", c)
	}
	time.Sleep(20 * time.Millisecond)
	if c := get(); c != http.StatusOK {
		t.Fatalf("db back: status %d", c)
	}
	if pings.Load() != 2 {
		t.Fatalf("pings = %d, want 2", pings.Load())
	}
}

func TestHealthReportsStorageOutage(t *testing.T) {
	storageErr := errors.New("s3 down")
	hc := &healthCheck{ttl: 0, ping: func(context.Context) error { return nil }, storage: func() error { return storageErr }}
	rec := httptest.NewRecorder()
	health(hc, "").ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "storage") {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
	storageErr = nil
	rec = httptest.NewRecorder()
	health(hc, "").ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("storage back: status %d", rec.Code)
	}
}

func TestSFUMetrics(t *testing.T) {
	var b strings.Builder
	writeSFUMetrics(&b, map[uuid.UUID]map[uuid.UUID]sfu.MediaState{
		uuid.New(): {uuid.New(): {Screen: true, Camera: true}, uuid.New(): {Camera: true}},
		uuid.New(): {uuid.New(): {Screen: true}},
	})
	for _, want := range []string{"mnema_sfu_video_rooms 2\n", "mnema_sfu_screen_shares 2\n", "mnema_sfu_cameras 2\n"} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("missing %q in\n%s", want, b.String())
		}
	}
}

func TestRouterCloseStopsBackgroundWork(t *testing.T) {
	r := &Router{}
	r.Close() // no cancel set: must not panic
	ctx, cancel := context.WithCancel(context.Background())
	r = &Router{cancel: cancel, Hub: ws.NewHub(nil, nil, nil, nil)}
	r.Close()
	if ctx.Err() == nil {
		t.Fatal("Close did not cancel the router context")
	}
	response := httptest.NewRecorder()
	r.Hub.HandleWebSocket(response, httptest.NewRequest(http.MethodGet, "/api/ws", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatal("Close did not stop WebSocket admission")
	}
}
