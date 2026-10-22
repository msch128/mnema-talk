package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/msch128/mnema-talk/internal/httpx"
)

// Once the global budget of unknown invite codes is spent, registration
// answers 429 before looking at the code, whatever the client address.
func TestRegisterStopsAtTheGlobalInviteMissBudget(t *testing.T) {
	h := NewHandler(&Sessions{Secret: strings.Repeat("s", 32), TTL: time.Hour}, nil)
	h.inviteMisses = httpx.NewRateLimiter(2, time.Hour)
	h.inviteMisses.Allow("misses")
	h.inviteMisses.Allow("misses")

	body := `{"username":"newcomer","password":"member-password-123","invite_code":"k7Rm2xQv9pLw"}`
	for _, ip := range []string{"203.0.113.1", "198.51.100.77", "192.0.2.5"} {
		r := httptest.NewRequest(http.MethodPost, "/api/auth/register", strings.NewReader(body))
		r.RemoteAddr = ip + ":1234"
		rec := httptest.NewRecorder()
		httpx.Handle(h.register).ServeHTTP(rec, r)
		if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
			t.Fatalf("%s: got %d, want 429 with Retry-After", ip, rec.Code)
		}
	}
}
