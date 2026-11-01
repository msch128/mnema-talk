package server

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"net"
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
	st.maxBytes = 3 << 20
	return st
}

func TestSpeedTestStreamsABoundedAmountOfIncompressibleBytes(t *testing.T) {
	h := speedHandler(smallSpeedTest())
	w := httptest.NewRecorder()
	if err := h.downloadSpeedTest(w, speedRequest(uuid.New())); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusOK || w.Body.Len() != 3<<20 {
		t.Fatalf("status %d, %d bytes", w.Code, w.Body.Len())
	}
	hdr := w.Header()
	if hdr.Get("Cache-Control") != "no-store" || hdr.Get("Content-Type") != "application/octet-stream" || hdr.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("headers: %v", hdr)
	}
	// Content-Encoding: identity tells a compressing proxy to leave the stream alone, and
	// X-Accel-Buffering: no tells nginx not to buffer it; both are what make the number honest.
	if hdr.Get("Content-Encoding") != "identity" || hdr.Get("X-Accel-Buffering") != "no" {
		t.Fatalf("anti-proxy headers missing: %v", hdr)
	}
	var z bytes.Buffer
	zw := gzip.NewWriter(&z)
	_, _ = zw.Write(w.Body.Bytes())
	_ = zw.Close()
	if z.Len() < w.Body.Len()*9/10 {
		t.Fatalf("the stream compresses to %d of %d bytes", z.Len(), w.Body.Len())
	}
}

// slowWriter takes its time over every write and can cancel the request.
type slowWriter struct {
	*httptest.ResponseRecorder
	delay      time.Duration
	writes     int
	cancelAt   int
	cancelFunc context.CancelFunc
}

func (s *slowWriter) Write(b []byte) (int, error) {
	s.writes++
	time.Sleep(s.delay)
	if s.cancelFunc != nil && s.writes == s.cancelAt {
		s.cancelFunc()
	}
	return s.ResponseRecorder.Write(b)
}

func TestSpeedTestStopsAtItsTimeLimit(t *testing.T) {
	st := smallSpeedTest()
	st.maxBytes, st.maxTime = 1<<30, 150*time.Millisecond
	w := &slowWriter{ResponseRecorder: httptest.NewRecorder(), delay: 10 * time.Millisecond}
	start := time.Now()
	if err := speedHandler(st).downloadSpeedTest(w, speedRequest(uuid.New())); err != nil {
		t.Fatal(err)
	}
	took := time.Since(start)
	if took < 150*time.Millisecond || took > 3*time.Second {
		t.Fatalf("the stream lasted %v, want about the 150ms limit", took)
	}
	if w.Body.Len() >= st.maxBytes || w.writes < 3 {
		t.Fatalf("%d bytes in %d writes: the time limit, not the size, should have ended it", w.Body.Len(), w.writes)
	}
}

func TestSpeedTestEndsWhenTheClientLeavesMidStream(t *testing.T) {
	st := smallSpeedTest()
	st.maxBytes, st.maxTime = 1<<30, time.Minute
	ctx, cancel := context.WithCancel(context.Background())
	r := speedRequest(uuid.New()).WithContext(auth.WithUser(ctx, &auth.User{ID: uuid.New()}))
	w := &slowWriter{ResponseRecorder: httptest.NewRecorder(), cancelAt: 3, cancelFunc: cancel}
	if err := speedHandler(st).downloadSpeedTest(w, r); err != nil {
		t.Fatal(err)
	}
	if w.writes != 3 {
		t.Fatalf("%d writes; the stream should stop right after the client left", w.writes)
	}
}

func TestSpeedTestIsLimitedPerMemberAndPerAddress(t *testing.T) {
	h := speedHandler(smallSpeedTest())
	me := uuid.New()
	run := func(id uuid.UUID, addr string) int {
		r := speedRequest(id)
		r.RemoteAddr = addr
		w := httptest.NewRecorder()
		_ = h.downloadSpeedTest(w, r)
		return w.Code
	}
	for i := 0; i < speedPerMember; i++ {
		if c := run(me, "203.0.113.1:1000"); c != http.StatusOK {
			t.Fatalf("test %d: %d", i, c)
		}
	}
	if c := run(me, "203.0.113.1:1000"); c != http.StatusTooManyRequests {
		t.Fatalf("over the member limit: %d", c)
	}
	// Other accounts from the same address share its budget: 6 in all.
	for i := speedPerMember; i < speedPerIP; i++ {
		if c := run(uuid.New(), "203.0.113.1:1000"); c != http.StatusOK {
			t.Fatalf("test %d of the address: %d", i, c)
		}
	}
	if c := run(uuid.New(), "203.0.113.1:2000"); c != http.StatusTooManyRequests {
		t.Fatalf("over the address limit: %d", c)
	}
	if c := run(uuid.New(), "198.51.100.9:1000"); c != http.StatusOK {
		t.Fatalf("another address: %d", c)
	}
}

func TestARefusedSpeedTestCostsNoBudget(t *testing.T) {
	h := speedHandler(smallSpeedTest())
	me := uuid.New()
	h.speed.slots <- struct{}{} // another test is running
	for i := 0; i < 20; i++ {
		w := httptest.NewRecorder()
		_ = h.downloadSpeedTest(w, speedRequest(me))
		if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") == "" {
			t.Fatalf("busy slot: %d", w.Code)
		}
	}
	<-h.speed.slots
	for i := 0; i < speedPerMember; i++ {
		w := httptest.NewRecorder()
		_ = h.downloadSpeedTest(w, speedRequest(me))
		if w.Code != http.StatusOK {
			t.Fatalf("after the busy refusals, test %d: %d; they must not have used the member's four tests", i, w.Code)
		}
	}
}

func TestSpeedTestStaysOutOfARunningCall(t *testing.T) {
	h := speedHandler(smallSpeedTest())
	running := true
	h.speed.callRunning = func() bool { return running }
	me := uuid.New()
	for i := 0; i < 10; i++ {
		w := httptest.NewRecorder()
		if err := h.downloadSpeedTest(w, speedRequest(me)); err == nil {
			t.Fatal("a test ran during a call")
		}
	}
	running = false
	w := httptest.NewRecorder()
	if err := h.downloadSpeedTest(w, speedRequest(me)); err != nil || w.Code != http.StatusOK || w.Body.Len() == 0 {
		t.Fatalf("after the call: %v %d", err, w.Code)
	}
}

// A client that connects and never reads must not hold the only slot: each
// write has a deadline.
func TestSpeedTestLetsGoOfAClientThatStopsReading(t *testing.T) {
	st := newSpeedTest()
	st.maxBytes, st.maxTime, st.writeWait = 1<<30, time.Minute, 200*time.Millisecond
	h := speedHandler(st)
	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(done)
		_ = h.downloadSpeedTest(w, speedRequest(uuid.New()))
	}))
	defer srv.Close()
	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("GET /api/webrtc/speedtest HTTP/1.1\r\nHost: x\r\n\r\n")); err != nil {
		t.Fatal(err)
	}
	// Read the header line only, then stop reading.
	if _, err := bufio.NewReader(conn).ReadString('\n'); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the handler still holds the slot long after the client stopped reading")
	}
	select {
	case h.speed.slots <- struct{}{}:
	default:
		t.Fatal("the slot was not released")
	}
}
