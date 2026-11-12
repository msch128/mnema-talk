package server

import (
	"net/http"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/msch128/mnema-talk/internal/config"
	"github.com/msch128/mnema-talk/internal/perm/permtest"
)

// bodyCappedInHandler are the routes that cap their body inside the handler,
// before the first read, instead of with the httpx.MaxBody middleware. Each is
// named with the line that does it.
var bodyCappedInHandler = map[string]string{
	// readRequest caps JSON at DefaultMaxBody and multipart at the media
	// limit before reading, and refuses other content types unread
	// (internal/webhook/execute_request.go:47,63).
	"POST /api/webhooks/{id}/{token}": "internal/webhook/execute_request.go",
}

// Every route that takes a body has a size cap (lane 8, "a body limit on
// every route"). It walks the real router and looks for the httpx.maxBody
// middleware in each body-taking route's stack; a route mounted outside the
// capped groups fails here unless it is listed above. The list may only shrink.
func TestEveryBodyTakingRouteHasASizeCap(t *testing.T) {
	cfg := &config.Config{AppEnv: "development", PublicURL: "http://127.0.0.1:8080", AllowedOrigins: []string{"http://127.0.0.1:8080"},
		JWTSecret: "body-limit-test-secret", SessionExpiryHours: 1, MetricsToken: "x", LinkPreviews: true, APIDocs: true, MaxUploadMB: 1}
	router, err := NewRouter(Deps{Config: cfg, Perms: permtest.NewFixture().Service()})
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	var missing []string
	capped := 0
	err = chi.Walk(router.Handler.(chi.Routes), func(method, route string, _ http.Handler, mws ...func(http.Handler) http.Handler) error {
		switch method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			return nil
		}
		if !strings.HasPrefix(route, "/api/") {
			return nil
		}
		for _, mw := range mws {
			if strings.Contains(runtime.FuncForPC(reflect.ValueOf(mw).Pointer()).Name(), "httpx.maxBody") {
				capped++
				return nil
			}
		}
		missing = append(missing, method+" "+route)
		return nil
	})
	if err != nil {
		t.Fatalf("chi.Walk: %v", err)
	}
	sort.Strings(missing)
	if capped < 100 {
		t.Fatalf("only %d capped routes found: the walk did not see the router", capped)
	}
	seen := map[string]bool{}
	for _, r := range missing {
		seen[r] = true
		if _, ok := bodyCappedInHandler[r]; !ok {
			t.Errorf("%s takes a body without a size cap", r)
		}
	}
	for r := range bodyCappedInHandler {
		if !seen[r] {
			t.Errorf("%s is listed as capped in its handler but now has the middleware (or is gone): drop it from the list", r)
		}
	}
	t.Logf("body-taking routes: %d with the middleware, %d capped in the handler", capped, len(missing))
}
