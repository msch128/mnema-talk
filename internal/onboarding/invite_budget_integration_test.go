//go:build integration

package onboarding

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/testutil"
)

const seededCode = "k7Rm2xQv9pLw"

func seedInvite(t *testing.T, p *db.Pool) {
	t.Helper()
	ctx := context.Background()
	if err := auth.EnsureAdminUser(ctx, p, "Herzog", "owner-password-123", true); err != nil {
		t.Fatal(err)
	}
	owner, _, err := auth.Login(ctx, p, "Herzog", "owner-password-123")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := auth.CreateInvite(ctx, p, owner.ID, seededCode, nil, nil); err != nil {
		t.Fatal(err)
	}
}

func previewFrom(r http.Handler, ip, code string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/invites/"+code, nil)
	req.RemoteAddr = ip + ":1234"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// The per-client budget is the only limiter and does not depend on whether the
// code exists: an over-budget client is refused for valid codes too, other
// clients are unaffected, and nothing is shared globally (no lockout, no
// hit/miss oracle).
func TestInvitePreviewBudgetIsPerClientAndIgnoresHitOrMiss(t *testing.T) {
	p := testutil.DB(t)
	testutil.Reset(t, p)
	seedInvite(t, p)
	h := NewHandler(p, nil)
	h.previewPerIP = httpx.NewRateLimiter(3, time.Hour)
	r := chi.NewRouter()
	h.MountPublic(r)

	for i := 0; i < 3; i++ {
		if w := previewFrom(r, "203.0.113.1", "unknown-code-x"); w.Code != http.StatusOK {
			t.Fatalf("lookup %d: %d", i, w.Code)
		}
	}
	for _, code := range []string{seededCode, "unknown-code-x"} {
		w := previewFrom(r, "203.0.113.1", code)
		if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") == "" {
			t.Fatalf("over-budget client, %q: %d, want 429 for hits and misses alike", code, w.Code)
		}
	}
	// Other clients, including another one that hammered, are unaffected.
	if w := previewFrom(r, "198.51.100.9", seededCode); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"valid":true`) {
		t.Fatalf("other client: %d %s", w.Code, w.Body)
	}
	for i := 0; i < 20; i++ {
		previewFrom(r, "192.0.2.7", "unknown-code-y")
	}
	if w := previewFrom(r, "198.51.100.10", seededCode); w.Code != http.StatusOK {
		t.Fatalf("a third client after another one exhausted its own budget: %d", w.Code)
	}
}
