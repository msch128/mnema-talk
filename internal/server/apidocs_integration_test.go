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

func TestAPIDocsNeedASession(t *testing.T) {
	a := newAppWithConfig(t, func(c *config.Config) { c.APIDocs = true })
	admin := a.seedAdmin()

	anon := a.anon()
	anon.http.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if res := anon.get("/api/docs"); res.status != http.StatusFound || res.header.Get("Location") != "/" {
		t.Fatalf("docs without session: %d -> %q", res.status, res.header.Get("Location"))
	}
	if res := anon.get("/api/openapi.json"); res.status != http.StatusUnauthorized {
		t.Fatalf("spec without session: %d", res.status)
	}

	spec := admin.get("/api/openapi.json")
	if spec.status != http.StatusOK || spec.header.Get("Content-Type") != "application/json" || spec.header.Get("Cache-Control") != "no-cache" {
		t.Fatalf("spec: %d %v", spec.status, spec.header)
	}
	var doc struct{ Paths map[string]any }
	if err := json.Unmarshal(spec.body, &doc); err != nil || doc.Paths["/api/admin/invites"] == nil {
		t.Fatalf("spec is not the API description: %v", err)
	}

	// The page is only embedded when the web app was built; either way a
	// member never gets the API's default-src 'none' page policy for HTML.
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
		if page.header.Get("Content-Security-Policy") != httpx.SPAContentSecurityPolicy || !strings.HasPrefix(page.header.Get("Content-Type"), "text/html") {
			t.Fatalf("docs page headers: %v", page.header)
		}
	case http.StatusServiceUnavailable:
	default:
		t.Fatalf("docs for a member: %d %s", page.status, page.body)
	}
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

func TestAPIDocsHandlerServesTheEmbeddedPage(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	page := []byte("<!doctype html><title>API</title>")
	rec := docsHandlerResponse(t, a, admin, page, true)
	if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), page) ||
		rec.Header().Get("Content-Security-Policy") != httpx.SPAContentSecurityPolicy ||
		!strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") || rec.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("docs page: %d %v", rec.Code, rec.Header())
	}
}

func TestAPIDocsHandlerReportsAMinimalBuildAndGuardsTheSession(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	if rec := docsHandlerResponse(t, a, admin, nil, true); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("build without the docs page: %d", rec.Code)
	}
	if rec := docsHandlerResponse(t, a, admin, []byte("secret docs"), false); rec.Code != http.StatusFound || rec.Header().Get("Location") != "/" || strings.Contains(rec.Body.String(), "secret") {
		t.Fatalf("docs without a session: %d %v", rec.Code, rec.Header())
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
