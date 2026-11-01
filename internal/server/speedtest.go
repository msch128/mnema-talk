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
	speedMaxBytes   = 16 << 20
	speedMaxTime    = 3 * time.Second
	speedChunk      = 64 << 10
	speedPerMember  = 4
	speedWindow     = 10 * time.Minute
	speedParallel   = 2
	speedRetryAfter = 5 * time.Second
)

// speedTest holds the limits of the speed test; tests shrink them.
type speedTest struct {
	limit    *httpx.RateLimiter
	slots    chan struct{}
	maxBytes int
	maxTime  time.Duration
	chunk    []byte
}

func newSpeedTest() speedTest {
	return speedTest{
		limit:    httpx.NewRateLimiter(speedPerMember, speedWindow).Named("speed_test"),
		slots:    make(chan struct{}, speedParallel),
		maxBytes: speedMaxBytes,
		maxTime:  speedMaxTime,
		chunk:    incompressible(speedChunk),
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
// @Description Backs the upload hint of "Test my connection": streams up to 16 MiB of noise or for at most three seconds, whichever ends first, so the browser can measure its download speed from the server. Signed-in members only; at most four per ten minutes per member and two at a time.
// @ID downloadSpeedTest
// @Tags Realtime
// @Produce application/octet-stream
// @Security cookieAuth
// @Success 200 {file} binary "A stream of bytes."
// @Header 200 {string} Cache-Control "no-store"
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many tests, or too many running."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/webrtc/speedtest [get]
func (h *systemHandler) downloadSpeedTest(w http.ResponseWriter, r *http.Request) error {
	st := &h.speed
	member := auth.UserFrom(r.Context())
	if ok, retry := st.limit.Allow(member.ID.String()); !ok {
		httpx.WriteRateLimited(w, retry)
		return nil
	}
	select {
	case st.slots <- struct{}{}:
		defer func() { <-st.slots }()
	default:
		httpx.WriteRateLimited(w, speedRetryAfter)
		return nil
	}
	hdr := w.Header()
	hdr.Set("Content-Type", "application/octet-stream")
	hdr.Set("Cache-Control", "no-store")
	hdr.Set("Content-Encoding", "identity")
	hdr.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)

	rc := http.NewResponseController(w)
	deadline := time.Now().Add(st.maxTime)
	_ = rc.SetWriteDeadline(deadline.Add(time.Second))
	for sent := 0; sent < st.maxBytes && time.Now().Before(deadline) && r.Context().Err() == nil; {
		n := min(len(st.chunk), st.maxBytes-sent)
		if _, err := w.Write(st.chunk[:n]); err != nil {
			return nil
		}
		sent += n
	}
	return nil
}
