//go:build integration

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/config"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/web"
)

func assertAnonymousDocsBlocked(t *testing.T, a *app) {
	t.Helper()
	anon := a.anon()
	anon.http.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res := anon.get("/api/docs")
	if res.status != http.StatusFound {
		t.Fatalf("docs without session status: %d, want 302", res.status)
	}
	if res.header.Get("Location") != "/" {
		t.Fatalf("docs without session location: %q, want /", res.header.Get("Location"))
	}
	if res := anon.get("/api/openapi.json"); res.status != http.StatusUnauthorized {
		t.Fatalf("spec without session: %d, want 401", res.status)
	}
}

func assertAdminOpenAPISpec(t *testing.T, admin *client) {
	t.Helper()
	spec := admin.get("/api/openapi.json")
	if spec.status != http.StatusOK {
		t.Fatalf("spec status: %d, want 200", spec.status)
	}
	if spec.header.Get("Content-Type") != "application/json" {
		t.Fatalf("spec content type: %q", spec.header.Get("Content-Type"))
	}
	if spec.header.Get("Cache-Control") != "no-cache" {
		t.Fatalf("spec cache control: %q", spec.header.Get("Cache-Control"))
	}
	var doc struct{ Paths map[string]any }
	if err := json.Unmarshal(spec.body, &doc); err != nil {
		t.Fatalf("spec JSON invalid: %v", err)
	}
	if doc.Paths["/api/admin/invites"] == nil {
		t.Fatal("spec missing /api/admin/invites path")
	}
}

func assertAdminDocsPage(t *testing.T, admin *client) {
	t.Helper()
	page := admin.get("/api/docs")
	wantPageStatus := http.StatusServiceUnavailable
	if web.APIDocsPage() != nil {
		wantPageStatus = http.StatusOK
	}
	if page.status != wantPageStatus {
		t.Fatalf("embedded docs status: got %d, want %d", page.status, wantPageStatus)
	}
	switch page.status {
	case http.StatusOK:
		if page.header.Get("Content-Security-Policy") != httpx.SPAContentSecurityPolicy {
			t.Fatalf("docs page CSP mismatch: %q", page.header.Get("Content-Security-Policy"))
		}
		if !strings.HasPrefix(page.header.Get("Content-Type"), "text/html") {
			t.Fatalf("docs page content type mismatch: %q", page.header.Get("Content-Type"))
		}
	case http.StatusServiceUnavailable:
	default:
		t.Fatalf("docs for a member: %d %s", page.status, page.body)
	}
}

func TestAPIDocsNeedASession(t *testing.T) {
	a := newAppWithConfig(t, func(c *config.Config) { c.APIDocs = true })
	admin := a.seedAdmin()
	assertAnonymousDocsBlocked(t, a)
	assertAdminOpenAPISpec(t, admin)
	assertAdminDocsPage(t, admin)
}

// docsHandlerResponse sends the admin's session to apiDocs built around an
// explicit page, so both embed variants (page built in, page omitted by a
// minimal frontend build) are exercised whatever web/dist holds.
func docsHandlerResponse(t *testing.T, a *app, member *client, page []byte, withSession bool) *httptest.ResponseRecorder {
	t.Helper()
	sessions := &auth.Sessions{DB: a.db, Secret: "integration-test-secret-integration-test", TTL: time.Hour}
	req := httptest.NewRequest(http.MethodGet, "/api/docs", nil)
	if withSession {
		for _, cookie := range member.http.Jar.Cookies(mustURL(a.srv.URL)) {
			req.AddCookie(cookie)
		}
	}
	rec := httptest.NewRecorder()
	apiDocs(sessions, page).ServeHTTP(rec, req)
	return rec
}

func assertDocsPageHeaders(t *testing.T, rec *httptest.ResponseRecorder, page []byte) {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("docs status = %d, want 200", rec.Code)
	}
	if !bytes.Equal(rec.Body.Bytes(), page) {
		t.Fatal("docs page content mismatch")
	}
	if rec.Header().Get("Content-Security-Policy") != httpx.SPAContentSecurityPolicy {
		t.Errorf("CSP = %q, want %q", rec.Header().Get("Content-Security-Policy"), httpx.SPAContentSecurityPolicy)
	}
	if !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
		t.Errorf("Content-Type = %q, want text/html prefix", rec.Header().Get("Content-Type"))
	}
	if rec.Header().Get("Cache-Control") != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache", rec.Header().Get("Cache-Control"))
	}
}

func TestAPIDocsHandlerServesTheEmbeddedPage(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	page := []byte("<!doctype html><title>API</title>")
	rec := docsHandlerResponse(t, a, admin, page, true)
	assertDocsPageHeaders(t, rec, page)
}

func TestAPIDocsHandlerReportsAMinimalBuildAndGuardsTheSession(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	if rec := docsHandlerResponse(t, a, admin, nil, true); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("build without the docs page: %d", rec.Code)
	}
	rec := docsHandlerResponse(t, a, admin, []byte("secret docs"), false)
	if rec.Code != http.StatusFound {
		t.Fatalf("docs without a session: code=%d, want 302", rec.Code)
	}
	if rec.Header().Get("Location") != "/" {
		t.Fatalf("redirect location = %q, want /", rec.Header().Get("Location"))
	}
	if strings.Contains(rec.Body.String(), "secret") {
		t.Fatal("secret leaked without a session")
	}
}

func TestAPIDocsCanBeTurnedOff(t *testing.T) {
	a := newAppWithConfig(t, func(c *config.Config) { c.APIDocs = false })
	admin := a.seedAdmin()
	for _, path := range []string{"/api/docs", "/api/openapi.json"} {
		if res := admin.get(path); res.status != http.StatusNotFound {
			t.Fatalf("%s with docs off: %d", path, res.status)
		}
	}
}
