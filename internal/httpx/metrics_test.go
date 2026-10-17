package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/msch128/mnema-talk/internal/metrics"
)

func TestMetricsMiddlewareLabelsByRoutePattern(t *testing.T) {
	r := chi.NewRouter()
	r.Use(Metrics)
	r.Get("/things/{id}", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
	r.Get("/upgrade", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusSwitchingProtocols) })

	for _, id := range []string{"1", "2", "3"} {
		r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/things/"+id, nil))
	}
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/upgrade", nil))
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/nope/123", nil))

	if got := metrics.HTTPRequests.With("/things/{id}", "4xx").Load(); got != 3 {
		t.Fatalf("pattern count = %d, want 3 (raw ids must not become labels)", got)
	}
	if metrics.HTTPRequests.With("/upgrade", "1xx").Load() != 1 {
		t.Fatal("upgrade not counted")
	}
	if metrics.HTTPRequests.With("unmatched", "4xx").Load() < 1 {
		t.Fatal("unmatched route not counted")
	}
	if metrics.HTTPInflight.Load() != 0 {
		t.Fatalf("in flight = %d after requests finished", metrics.HTTPInflight.Load())
	}
}

func TestMetricsMiddlewareWithoutRouter(t *testing.T) {
	h := Metrics(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/bare", nil))
	if metrics.HTTPRequests.With("unmatched", "2xx").Load() < 1 {
		t.Fatal("request without a router not counted")
	}
}

func TestRateLimitHitsAreCountedByLimiterName(t *testing.T) {
	named := NewRateLimiter(1, time.Minute).Named("test-named")
	unnamed := NewRateLimiter(1, time.Minute)
	key := func(*http.Request) string { return "k" }
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {})
	for _, h := range []http.Handler{named.By(key)(ok), unnamed.By(key)(ok)} {
		for range 3 {
			h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
		}
	}
	if got := metrics.RateLimitHits.With("test-named").Load(); got != 2 {
		t.Fatalf("named hits = %d, want 2", got)
	}
	before := metrics.RateLimitHits.With("other").Load()
	WriteRateLimited(httptest.NewRecorder(), time.Second)
	if metrics.RateLimitHits.With("other").Load() != before+1 {
		t.Fatal("direct WriteRateLimited not counted")
	}
}
