//go:build integration

package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
)

func postRegister(h *Handler, username, code string) *httptest.ResponseRecorder {
	body := `{"username":"` + username + `","password":"member-password-123","invite_code":"` + code + `"}`
	r := httptest.NewRequest(http.MethodPost, "/api/auth/register", strings.NewReader(body))
	r.RemoteAddr = "203.0.113.1:1234"
	rec := httptest.NewRecorder()
	httpx.Handle(h.register).ServeHTTP(rec, r)
	return rec
}

// With the global budget of unknown codes spent, unknown codes get 429 but a
// valid invite still registers (AUTH-04).
func TestRegisterGlobalMissBudgetNeverBlocksValidInvites(t *testing.T) {
	p, _, _ := authFixture(t)
	h := NewHandler(&Sessions{DB: p, Secret: secret, TTL: time.Hour}, &events.Recorder{})
	h.inviteMisses = httpx.NewRateLimiter(2, time.Hour)

	for i := 0; i < 2; i++ {
		if rec := postRegister(h, "guesser"+string(rune('a'+i)), "unknown-code-xyz"); rec.Code != http.StatusBadRequest {
			t.Fatalf("miss %d: %d", i, rec.Code)
		}
	}
	rec := postRegister(h, "guesserz", "unknown-code-xyz")
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("unknown code after the budget: %d, want 429", rec.Code)
	}
	if rec := postRegister(h, "newcomer", "member-invite"); rec.Code != http.StatusCreated {
		t.Fatalf("valid invite while the budget is spent: %d %s", rec.Code, rec.Body)
	}
}
