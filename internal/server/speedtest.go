package server

import (
	"net/http"
	"time"

	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// The download speed test behind "Test my connection": a member's browser reads
// a short stream from the server and divides what arrived by the time it took.
// From outside the server's network that approximates the server's upload.
// It is bounded in size and time, for signed-in members only, and rate limited
// per member and in parallel, so it is no way to load the server or to
// amplify anything: the bytes go to the member who asked.
const (
	speedMaxBytes  = 16 << 20
	speedMaxTime   = 3 * time.Second
	speedChunk     = 1 << 20
	speedWriteWait = 2 * time.Second
	speedPerMember = 4
	speedPerIP     = 6
	speedWindow    = 10 * time.Minute
	// One test at a time: it can fill a home uplink, so two in parallel would
	// be twice the harm.
	speedParallel   = 1
	speedRetryAfter = 5 * time.Second
)

// speedTest holds the limits of the speed test; tests shrink them.
type speedTest struct {
	limit     *httpx.RateLimiter
	ipLimit   *httpx.RateLimiter
	slots     chan struct{}
	maxBytes  int
	maxTime   time.Duration
	writeWait time.Duration
	chunk     []byte
	// callRunning says whether a Talk has participants; the test then stays
	// out of their way.
	callRunning func() bool
}

func newSpeedTest() speedTest {
	return speedTest{
		limit:     httpx.NewRateLimiter(speedPerMember, speedWindow).Named("speed_test"),
		ipLimit:   httpx.NewRateLimiter(speedPerIP, speedWindow).Named("speed_test_ip"),
		slots:     make(chan struct{}, speedParallel),
		maxBytes:  speedMaxBytes,
		maxTime:   speedMaxTime,
		writeWait: speedWriteWait,
		chunk:     incompressible(speedChunk),
	}
}

// incompressible is n bytes that a compressing proxy cannot shrink (a simple
// xorshift stream; nothing secret, only noise).
func incompressible(n int) []byte {
	b := make([]byte, n)
	x := uint64(0x9E3779B97F4A7C15)
	for i := range b {
		x ^= x << 13
		x ^= x >> 7
		x ^= x << 17
		b[i] = byte(x)
	}
	return b
}

// downloadSpeedTest handles GET /api/webrtc/speedtest.
//
// @Summary Download speed test
// @Description Backs the upload hint of "Test my connection": streams up to 16 MiB of noise or for at most three seconds, whichever ends first, so the browser can measure its download speed from the server. The speed is the one to the nearest reverse proxy or tunnel, not necessarily to the server itself. Signed-in members only; at most four per ten minutes per member and six per address, one at a time. A busy slot or an exhausted budget answers 429 and costs nothing; while a Talk has participants it answers 503 so the test does not disturb the call.
// @ID downloadSpeedTest
// @Tags Realtime
// @Produce application/octet-stream
// @Security cookieAuth
// @Success 200 {file} binary "A stream of bytes."
// @Header 200 {string} Cache-Control "no-store"
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many tests, or one is running."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Failure 503 {object} httpx.ErrorResponse "UNAVAILABLE: a Talk has participants; the test would disturb the call."
// @Router /api/webrtc/speedtest [get]
func (h *systemHandler) downloadSpeedTest(w http.ResponseWriter, r *http.Request) error {
	st := &h.speed
	if st.callRunning != nil && st.callRunning() {
		return httpx.ErrUnavailable("a Talk is running; the speed test would disturb it")
	}
	select {
	case st.slots <- struct{}{}:
		defer func() { <-st.slots }()
	default:
		httpx.WriteRateLimited(w, speedRetryAfter)
		return nil
	}
	// Refused requests spend no budget (TryAllow); the address goes first so a
	// few accounts behind one address cannot keep the slot busy.
	ip := httpx.ClientIPKey(r)
	if ok, retry := st.ipLimit.TryAllow(ip); !ok {
		httpx.WriteRateLimited(w, retry)
		return nil
	}
	if ok, retry := st.limit.TryAllow(auth.UserFrom(r.Context()).ID.String()); !ok {
		st.ipLimit.Refund(ip)
		httpx.WriteRateLimited(w, retry)
		return nil
	}
	st.stream(w, r)
	return nil
}

// stream writes noise until the size or time limit, or until the client
// leaves or stops reading (each write has its own deadline, so a slow client
// ends the test with a low speed instead of holding the slot).
func (st *speedTest) stream(w http.ResponseWriter, r *http.Request) {
	hdr := w.Header()
	hdr.Set("Content-Type", "application/octet-stream")
	hdr.Set("Cache-Control", "no-store")
	hdr.Set("Content-Encoding", "identity")
	hdr.Set("X-Accel-Buffering", "no")
	hdr.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)

	rc := http.NewResponseController(w)
	deadline := time.Now().Add(st.maxTime)
	for sent := 0; sent < st.maxBytes && time.Now().Before(deadline) && r.Context().Err() == nil; {
		_ = rc.SetWriteDeadline(time.Now().Add(st.writeWait))
		n := min(len(st.chunk), st.maxBytes-sent)
		if _, err := w.Write(st.chunk[:n]); err != nil {
			return
		}
		sent += n
	}
}
