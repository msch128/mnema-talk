package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/msch128/mnema-talk/internal/httpx"
)

// memberLimits wires the members' limiters the way mountAuthenticated does,
// with every request coming from the same user.
func memberLimits() http.Handler {
	byUser := func(*http.Request) string { return "user-1" }
	reads := httpx.NewRateLimiter(apiReadsPerMinute, time.Minute).Named("api_user_read")
	writes := httpx.NewRateLimiter(apiWritesPerMinute, time.Minute).Named("api_user")
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	return httpx.BySafeMethod(reads.By(byUser), writes.By(byUser))(ok)
}

func status(h http.Handler, method string) int {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, "/api/channels/x/posts", nil))
	return rec.Code
}

func TestBrowsingBurstIsNotLimited(t *testing.T) {
	h := memberLimits()
	// Opening and closing forum posts quickly: a post list and a thread each time.
	for i := 0; i < 200; i++ {
		if got := status(h, http.MethodGet); got != http.StatusNoContent {
			t.Fatalf("read %d answered %d", i+1, got)
		}
	}
}

func TestReadsAreStillBoundedAndDoNotUseUpWrites(t *testing.T) {
	h := memberLimits()
	for i := 0; i < apiReadsPerMinute; i++ {
		if got := status(h, http.MethodGet); got != http.StatusNoContent {
			t.Fatalf("read %d answered %d", i+1, got)
		}
	}
	if got := status(h, http.MethodGet); got != http.StatusTooManyRequests {
		t.Fatalf("a runaway reader was not stopped: %d", got)
	}
	// Writes keep their own budget, and it still ends.
	for i := 0; i < apiWritesPerMinute; i++ {
		if got := status(h, http.MethodPost); got != http.StatusNoContent {
			t.Fatalf("write %d answered %d", i+1, got)
		}
	}
	if got := status(h, http.MethodPost); got != http.StatusTooManyRequests {
		t.Fatalf("writes are not limited: %d", got)
	}
}
