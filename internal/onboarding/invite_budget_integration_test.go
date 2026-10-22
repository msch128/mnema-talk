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
	if _, err := auth.CreateInvite(ctx, p, owner.ID, "k7Rm2xQv9pLw", nil, nil); err != nil {
		t.Fatal(err)
	}
}

func preview(h *Handler, code string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, "/api/invites/"+code, nil)
	rc := chi.NewRouteContext()
	rc.URLParams.Add("code", code)
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rc))
	w := httptest.NewRecorder()
	httpx.Handle(h.previewInvite).ServeHTTP(w, r)
	return w
}

// With the global budget of unknown codes spent, unknown codes are refused
// but a valid invite still previews: exhausting the budget must not lock
// invitees out (AUTH-04).
func TestInvitePreviewGlobalMissBudgetNeverBlocksValidCodes(t *testing.T) {
	p := testutil.DB(t)
	testutil.Reset(t, p)
	seedInvite(t, p)
	h := NewHandler(p, nil)
	h.lookupMisses = httpx.NewRateLimiter(3, time.Hour)

	for i := 0; i < 3; i++ {
		if w := preview(h, "unknown-code-"+string(rune('a'+i))); w.Code != http.StatusOK {
			t.Fatalf("miss %d: %d", i, w.Code)
		}
	}
	w := preview(h, "unknown-code-z")
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") == "" {
		t.Fatalf("unknown code after the budget: %d retry-after %q, want 429", w.Code, w.Header().Get("Retry-After"))
	}
	if w := preview(h, "k7Rm2xQv9pLw"); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"valid":true`) {
		t.Fatalf("valid code while the budget is spent: %d %s", w.Code, w.Body)
	}
}

// The per-client miss budget is the primary limit on unknown codes.
func TestInvitePreviewPerClientMissBudget(t *testing.T) {
	p := testutil.DB(t)
	testutil.Reset(t, p)
	seedInvite(t, p)
	h := NewHandler(p, nil)
	h.missPerIP = httpx.NewRateLimiter(2, time.Hour)
	for i := 0; i < 2; i++ {
		if w := preview(h, "unknown-code-"+string(rune('a'+i))); w.Code != http.StatusOK {
			t.Fatalf("miss %d: %d", i, w.Code)
		}
	}
	if w := preview(h, "unknown-code-z"); w.Code != http.StatusTooManyRequests {
		t.Fatalf("third miss from one client: %d", w.Code)
	}
	for i := 0; i < 10; i++ {
		if w := preview(h, "k7Rm2xQv9pLw"); w.Code != http.StatusOK {
			t.Fatalf("hit %d: %d", i, w.Code)
		}
	}
}
