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

func TestMetricsRequiresBearerToken(t *testing.T) {
	const token = "test-metrics-token-0123456789"
	h := requireBearer(token, metricsHandler(nil, nil, nil))
	for _, tc := range []struct {
		auth string
		want int
	}{
		{"", http.StatusUnauthorized},
		{"Bearer wrong", http.StatusUnauthorized},
		{token, http.StatusUnauthorized},
		{"Bearer " + token, http.StatusOK},
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/metrics", nil)
		if tc.auth != "" {
			req.Header.Set("Authorization", tc.auth)
		}
		h.ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Fatalf("auth %q: got %d, want %d", tc.auth, rec.Code, tc.want)
		}
	}
}
