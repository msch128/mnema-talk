package authn

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/msch128/mnema-talk/internal/auth"

	"github.com/msch128/mnema-talk/internal/httpx"
)

func TestLoginAnswers503WithRetryAfterWhenBusy(t *testing.T) {
	h := newLoginHandler()
	h.verify = func(context.Context, string, string) (*auth.User, int, error) { return nil, 0, auth.ErrServerBusy }
	rec := postLoginWith(h, "203.0.113.5", "cool_guy", "whatever-password")
	if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("got %d retry-after %q, want 503 with Retry-After", rec.Code, rec.Header().Get("Retry-After"))
	}
	if locked, _ := h.loginFailures.IsLockedOut("203.0.113.5|cool_guy"); locked || h.loginFailures.Len() != 0 {
		t.Fatal("a busy server must not count as a failed login")
	}
}

func TestImplausibleUsernameDoesNoHashing(t *testing.T) {
	release := auth.OccupyHashingSlots()
	t.Cleanup(release)
	h := newLoginHandler()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"username":"has space","password":"x"}`))
	done := make(chan struct{})
	go func() { httpx.Handle(h.login).ServeHTTP(rec, req); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("an impossible username waited for a hashing slot")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401", rec.Code)
	}
}
