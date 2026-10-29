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
	if err := auth.EnsureAdminUser(ctx, p, "cool_guy", "owner-password-123", true); err != nil {
		t.Fatal(err)
	}
	owner, _, err := auth.Login(ctx, p, "cool_guy", "owner-password-123")
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

// Rotating /64 prefixes inside one /48 shares one budget.
func TestInvitePreviewBudgetAggregatesPerIPv6Site(t *testing.T) {
	p := testutil.DB(t)
	testutil.Reset(t, p)
	seedInvite(t, p)
	h := NewHandler(p, nil)
	h.previewPerSite = httpx.NewRateLimiter(4, time.Hour)
	r := chi.NewRouter()
	h.MountPublic(r)
	for i := 0; i < 4; i++ {
		ip := "2001:db8:1:" + string(rune('a'+i)) + "::1"
		if w := previewFrom(r, ip, "unknown-code-x"); w.Code != http.StatusOK {
			t.Fatalf("prefix %d: %d", i, w.Code)
		}
	}
	if w := previewFrom(r, "2001:db8:1:ffff::9", seededCode); w.Code != http.StatusTooManyRequests {
		t.Fatalf("another /64 of the same /48: %d, want 429", w.Code)
	}
	if w := previewFrom(r, "2001:db8:2::1", seededCode); w.Code != http.StatusOK {
		t.Fatalf("a different /48: %d", w.Code)
	}
}

// A legacy custom code below today's floor answers like an unknown code, and a
// generated code works.
func TestInvitePreviewTreatsWeakLegacyCodesAsUnknown(t *testing.T) {
	p := testutil.DB(t)
	testutil.Reset(t, p)
	seedInvite(t, p)
	owner, _, err := auth.Login(context.Background(), p, "cool_guy", "owner-password-123")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Exec(context.Background(), `INSERT INTO invites (code, created_by) VALUES ('welcome', $1)`, owner.ID); err != nil {
		t.Fatal(err)
	}
	generated, err := auth.CreateInvite(context.Background(), p, owner.ID, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandler(p, nil)
	r := chi.NewRouter()
	h.MountPublic(r)
	weak := previewFrom(r, "203.0.113.1", "welcome")
	unknown := previewFrom(r, "203.0.113.2", "nothere")
	if weak.Code != http.StatusOK || weak.Body.String() != unknown.Body.String() || strings.Contains(weak.Body.String(), `"valid":true`) {
		t.Fatalf("weak legacy code: %d %s, want the unknown-code answer %s", weak.Code, weak.Body, unknown.Body)
	}
	if w := previewFrom(r, "203.0.113.3", generated.Code); !strings.Contains(w.Body.String(), `"valid":true`) {
		t.Fatalf("generated code: %s", w.Body)
	}
}
