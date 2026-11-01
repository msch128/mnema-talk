package server

import (
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
)

func speedHandler(st speedTest) *systemHandler { return &systemHandler{speed: st} }

func speedRequest(id uuid.UUID) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/api/webrtc/speedtest", nil)
	return r.WithContext(auth.WithUser(r.Context(), &auth.User{ID: id}))
}

func smallSpeedTest() speedTest {
	st := newSpeedTest()
	st.maxBytes = 200 << 10
	return st
}

func TestSpeedTestStreamsABoundedAmountOfIncompressibleBytes(t *testing.T) {
	h := speedHandler(smallSpeedTest())
	w := httptest.NewRecorder()
	if err := h.downloadSpeedTest(w, speedRequest(uuid.New())); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusOK || w.Body.Len() != 200<<10 {
		t.Fatalf("status %d, %d bytes", w.Code, w.Body.Len())
	}
	hdr := w.Header()
	if hdr.Get("Cache-Control") != "no-store" || hdr.Get("Content-Encoding") != "identity" || hdr.Get("Content-Type") != "application/octet-stream" || hdr.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("headers: %v", hdr)
	}
	var z bytes.Buffer
	zw := gzip.NewWriter(&z)
	_, _ = zw.Write(w.Body.Bytes())
	_ = zw.Close()
	if z.Len() < w.Body.Len()*9/10 {
		t.Fatalf("the stream compresses to %d of %d bytes: a compressing proxy would falsify the speed", z.Len(), w.Body.Len())
	}
}

func TestSpeedTestStopsAtItsTimeLimit(t *testing.T) {
	st := smallSpeedTest()
	st.maxBytes, st.maxTime = 1<<30, 0
	w := httptest.NewRecorder()
	if err := speedHandler(st).downloadSpeedTest(w, speedRequest(uuid.New())); err != nil {
		t.Fatal(err)
	}
	if w.Body.Len() >= 1<<30 {
		t.Fatal("the time limit did not end the stream")
	}
}

func TestSpeedTestIsLimitedPerMemberAndInParallel(t *testing.T) {
	h := speedHandler(smallSpeedTest())
	me := uuid.New()
	for i := 0; i < speedPerMember; i++ {
		w := httptest.NewRecorder()
		_ = h.downloadSpeedTest(w, speedRequest(me))
		if w.Code != http.StatusOK {
			t.Fatalf("test %d: %d", i, w.Code)
		}
	}
	w := httptest.NewRecorder()
	_ = h.downloadSpeedTest(w, speedRequest(me))
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") == "" {
		t.Fatalf("over the member limit: %d", w.Code)
	}

	// Both slots taken: another member is turned away too.
	for i := 0; i < speedParallel; i++ {
		h.speed.slots <- struct{}{}
	}
	w = httptest.NewRecorder()
	_ = h.downloadSpeedTest(w, speedRequest(uuid.New()))
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("all slots busy: %d", w.Code)
	}
	<-h.speed.slots
	w = httptest.NewRecorder()
	_ = h.downloadSpeedTest(w, speedRequest(uuid.New()))
	if w.Code != http.StatusOK {
		t.Fatalf("a slot is free again: %d", w.Code)
	}
}

func TestSpeedTestEndsWhenTheClientGoesAway(t *testing.T) {
	st := smallSpeedTest()
	st.maxBytes, st.maxTime = 1<<30, time.Minute
	r := speedRequest(uuid.New())
	ctx, cancel := contextWithCancel(r)
	cancel()
	done := make(chan struct{})
	go func() {
		_ = speedHandler(st).downloadSpeedTest(httptest.NewRecorder(), r.WithContext(ctx))
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the stream kept going after the client left")
	}
}

func contextWithCancel(r *http.Request) (context.Context, context.CancelFunc) {
	return context.WithCancel(r.Context())
}
