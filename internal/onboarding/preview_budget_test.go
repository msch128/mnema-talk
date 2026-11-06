package onboarding

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/msch128/mnema-talk/internal/httpx"
)

func previewHandler(perIP, perSite int) (*Handler, http.Handler) {
	h := &Handler{
		previewPerIP:   httpx.NewRateLimiter(perIP, time.Hour),
		previewPerSite: httpx.NewRateLimiter(perSite, time.Hour),
	}
	return h, h.limitPreview(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
}

func previewStatus(h http.Handler, ip string) int {
	req := httptest.NewRequest(http.MethodGet, "/invites/x", nil)
	req.RemoteAddr = "[" + ip + "]:1234"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code
}

// Two /64 prefixes of one /48 share the site budget.
const (
	prefixA = "2001:db8:1:a::1"
	prefixB = "2001:db8:1:b::1"
)

// A caller who is over its own budget is refused before the site budget is
// touched, so one noisy address cannot lock its neighbours out of the site.
func TestPreviewRefusedByTheCallersOwnBudgetDoesNotChargeTheSite(t *testing.T) {
	_, h := previewHandler(2, 4)
	for i := 0; i < 20; i++ {
		previewStatus(h, prefixA)
	}
	for i := 0; i < 2; i++ {
		if code := previewStatus(h, prefixB); code != http.StatusOK {
			t.Fatalf("neighbour request %d: %d, want 200", i, code)
		}
	}
}

// A request the site budget refuses gives the caller's own hit back.
func TestPreviewRefusedBySiteBudgetDoesNotChargeTheCaller(t *testing.T) {
	h, mw := previewHandler(2, 2)
	for i := 0; i < 2; i++ {
		previewStatus(mw, prefixB)
	}
	for i := 0; i < 10; i++ {
		if code := previewStatus(mw, prefixA); code != http.StatusTooManyRequests {
			t.Fatalf("request %d over the site budget: %d", i, code)
		}
	}
	for i := 0; i < 2; i++ {
		if ok, _ := h.previewPerIP.TryAllow(httpx.ClientIPKey(requestFrom(prefixA))); !ok {
			t.Fatalf("hit %d: refused requests used up the caller's own budget", i)
		}
	}
}

func requestFrom(ip string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "[" + ip + "]:1234"
	return req
}
