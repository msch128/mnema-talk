package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/msch128/mnema-talk/internal/config"
	"github.com/msch128/mnema-talk/internal/perm/permtest"
)

func TestNewRouterRejectsInvalidDependencies(t *testing.T) {
	for _, cfg := range []*config.Config{
		{TrustedProxies: []string{"not-a-cidr"}},
		{UpdaterToken: strings.Repeat("test-", 8), UpdaterURL: "ftp://example.invalid"},
	} {
		router, err := NewRouter(Deps{Config: cfg, Perms: permtest.NewFixture().Service()})
		if err == nil || router != nil {
			t.Fatalf("router=%v err=%v", router, err)
		}
	}
}

func TestRouterRouteErrorsAreJSON(t *testing.T) {
	router, err := NewRouter(Deps{Config: &config.Config{AllowedOrigins: []string{"http://example.test"}, JWTSecret: "router-test-secret-router-test-secret"}, Perms: permtest.NewFixture().Service()})
	if err != nil {
		t.Fatal(err)
	}
	defer router.Close()
	for _, tc := range []struct {
		method, path string
		status       int
		code         string
	}{
		{http.MethodGet, "/api/does-not-exist", http.StatusNotFound, "NOT_FOUND"},
		{http.MethodPost, "/api/health", http.StatusMethodNotAllowed, "INVALID_INPUT"},
		{http.MethodGet, "/api/health", http.StatusServiceUnavailable, "UNAVAILABLE"},
	} {
		r := httptest.NewRequest(tc.method, tc.path, nil)
		r.Header.Set("Origin", "http://example.test")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, r)
		if rec.Code != tc.status || !strings.Contains(rec.Body.String(), tc.code) || !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
			t.Fatalf("%s %s: %d %s", tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}
	(&Router{}).Close()
}

func TestProductionRouterHasNoNativeRoute(t *testing.T) {
	router, err := NewRouter(Deps{Config: &config.Config{AllowedOrigins: []string{"http://example.test"}, JWTSecret: "router-test-secret-router-test-secret"}, Perms: permtest.NewFixture().Service()})
	if err != nil {
		t.Fatal(err)
	}
	defer router.Close()
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/native/v1/", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /api/native/v1/ = %d, want 404", rec.Code)
	}
}
