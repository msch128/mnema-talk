package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMetricsEndpoint(t *testing.T) {
	handler := metricsHandler(nil, nil, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/metrics", nil)

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "mnema_up 1") {
		t.Fatalf("expected metrics to contain mnema_up 1, got:\n%s", body)
	}
	if !strings.Contains(body, "go_goroutines") {
		t.Fatalf("expected metrics to contain go_goroutines, got:\n%s", body)
	}
	if !strings.Contains(body, "go_memstats_alloc_bytes") {
		t.Fatalf("expected metrics to contain go_memstats_alloc_bytes, got:\n%s", body)
	}
}
