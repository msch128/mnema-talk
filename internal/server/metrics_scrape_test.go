package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// A scrape reads the process and its dependencies once: the read stops the
// world and probes object storage.
func TestMetricsScrapeReadsOnce(t *testing.T) {
	probes := 0
	src := &opsSource{hasStore: true, storage: func() error { probes++; return nil }}
	w := httptest.NewRecorder()
	metricsHandler(src).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/metrics", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	if probes != 1 {
		t.Fatalf("storage probed %d times in one scrape, want 1", probes)
	}
}
