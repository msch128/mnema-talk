package chat

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

var updateRoutes = flag.Bool("update-routes", false, "rewrite testdata/routes.golden")

// walkRoutes lists "METHOD path" for every route registered on r, sorted.
func walkRoutes(t *testing.T, r chi.Router) []string {
	t.Helper()
	var routes []string
	err := chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		routes = append(routes, method+" "+strings.ReplaceAll(route, "/*/", "/"))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(routes)
	return routes
}

// TestRouteTableIsStable pins the method and path of every route the chat
// handler mounts. Moving code between packages must not add, drop or rename
// a route; an intended API change updates the golden file with
// `go test ./internal/chat -run TestRouteTableIsStable -update-routes`.
func TestRouteTableIsStable(t *testing.T) {
	// Every optional feature switched on, so no route is left out.
	h := &Handler{Talks: &TalkManager{}}
	member, admin, bot := chi.NewRouter(), chi.NewRouter(), chi.NewRouter()
	h.Mount(member)
	h.MountAdmin(admin)
	h.MountBot(bot, func(next http.Handler) http.Handler { return next })

	var lines []string
	for _, group := range []struct {
		name string
		r    chi.Router
	}{{"member", member}, {"admin", admin}, {"bot", bot}} {
		for _, route := range walkRoutes(t, group.r) {
			lines = append(lines, fmt.Sprintf("%s %s", group.name, route))
		}
	}
	got := strings.Join(lines, "\n") + "\n"

	path := filepath.Join("testdata", "routes.golden")
	if *updateRoutes {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("route table changed; diff against %s (rerun with -update-routes if intended):\n--- got ---\n%s", path, got)
	}
}
