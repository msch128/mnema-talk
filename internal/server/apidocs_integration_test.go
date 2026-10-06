//go:build integration

package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/msch128/mnema-talk/internal/config"
	"github.com/msch128/mnema-talk/internal/httpx"
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

func TestAPIDocsCanBeTurnedOff(t *testing.T) {
	a := newAppWithConfig(t, func(c *config.Config) { c.APIDocs = false })
	admin := a.seedAdmin()
	for _, path := range []string{"/api/docs", "/api/openapi.json"} {
		if res := admin.get(path); res.status != http.StatusNotFound {
			t.Fatalf("%s with docs off: %d", path, res.status)
		}
	}
}
