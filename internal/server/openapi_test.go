package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/msch128/mnema-talk/internal/config"
)

// specPath is relative to this package's directory (go test runs there).
const specPath = "../../api/openapi.json"

// openAPIMethods are the path-item keys that name HTTP operations.
var openAPIMethods = map[string]bool{
	"get": true, "put": true, "post": true, "delete": true,
	"options": true, "head": true, "patch": true, "trace": true,
}

// TestOpenAPICoversRoutes fails when a registered /api route is missing from
// api/openapi.json, or the spec documents a route the router does not serve.
// The router is built without a database: route registration does not touch it.
func TestOpenAPICoversRoutes(t *testing.T) {
	registered := registeredRoutes(t)
	documented := documentedRoutes(t)

	var missing, stale []string
	for r := range registered {
		if !documented[r] {
			missing = append(missing, r)
		}
	}
	for r := range documented {
		if !registered[r] {
			stale = append(stale, r)
		}
	}
	sort.Strings(missing)
	sort.Strings(stale)
	if len(missing) > 0 {
		t.Errorf("routes registered but not documented in %s:\n  %s", specPath, strings.Join(missing, "\n  "))
	}
	if len(stale) > 0 {
		t.Errorf("operations documented in %s but not registered:\n  %s", specPath, strings.Join(stale, "\n  "))
	}
}

func registeredRoutes(t *testing.T) map[string]bool {
	t.Helper()
	return routesOf(t, true)
}

// The API reference is off unless API_DOCS_ENABLED is set.
func TestAPIDocsAreOffByDefault(t *testing.T) {
	routes := routesOf(t, false)
	for _, r := range []string{"GET /api/docs", "GET /api/openapi.json"} {
		if routes[r] {
			t.Errorf("%s is mounted although API docs are disabled", r)
		}
	}
}

func routesOf(t *testing.T, apiDocs bool) map[string]bool {
	t.Helper()
	cfg := &config.Config{
		AppEnv:             "development",
		PublicURL:          "http://127.0.0.1:8080", // an IP literal keeps link-preview setup off DNS
		AllowedOrigins:     []string{"http://127.0.0.1:8080"},
		JWTSecret:          "openapi-test-secret",
		SessionExpiryHours: 1,
		MetricsToken:       "x", // registers /api/metrics
		LinkPreviews:       true,
		APIDocs:            apiDocs, // registers /api/docs and /api/openapi.json
		MaxUploadMB:        1,
	}
	router, err := NewRouter(Deps{Config: cfg})
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	routes := map[string]bool{}
	err = chi.Walk(router.Handler.(chi.Routes), func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if !strings.HasPrefix(route, "/api/") {
			return nil // the SPA catch-all
		}
		routes[method+" "+route] = true
		return nil
	})
	if err != nil {
		t.Fatalf("chi.Walk: %v", err)
	}
	if len(routes) == 0 {
		t.Fatal("no /api routes found")
	}
	return routes
}

func documentedRoutes(t *testing.T) map[string]bool {
	t.Helper()
	raw, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	var spec struct {
		OpenAPI string                                `json:"openapi"`
		Paths   map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatalf("parse spec: %v", err)
	}
	if !strings.HasPrefix(spec.OpenAPI, "3.1.") {
		t.Fatalf("openapi = %q, want 3.1.x", spec.OpenAPI)
	}
	routes := map[string]bool{}
	for path, item := range spec.Paths {
		for key := range item {
			if openAPIMethods[key] {
				routes[fmt.Sprintf("%s %s", strings.ToUpper(key), path)] = true
			}
		}
	}
	return routes
}
