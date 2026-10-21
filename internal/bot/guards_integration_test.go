//go:build integration

package bot

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/perm"
)

// Every management route and the guard it uses. Mutating routes (and the ones
// that reveal tokens) must go through loadActing: the caller needs at least
// the bot's rights. Read-only routes use load (the hierarchy). A new route
// must be listed here, so none can slip in unguarded.
var routeGuards = map[string]string{
	"GET /admin/bots":                                   "none",
	"POST /admin/bots":                                  "none",
	"GET /admin/bots/{id}":                              "load",
	"PATCH /admin/bots/{id}":                            "acting",
	"DELETE /admin/bots/{id}":                           "acting",
	"POST /admin/bots/{id}/avatar":                      "acting",
	"PUT /admin/bots/{id}/roles":                        "acting",
	"GET /admin/bots/{id}/access":                       "load",
	"GET /admin/bots/{id}/tokens":                       "acting",
	"POST /admin/bots/{id}/tokens":                      "acting",
	"POST /admin/bots/{id}/tokens/{tokenID}/regenerate": "acting",
	"DELETE /admin/bots/{id}/tokens/{tokenID}":          "acting",
	"GET /admin/bots/{id}/commands":                     "load",
	"PATCH /admin/bots/{id}/commands/{commandID}":       "acting",
	"DELETE /admin/bots/{id}/commands/{commandID}":      "acting",
}

func seedManagerAndElevatedBot(t *testing.T, e *env) (*auth.User, fixtureBot) {
	t.Helper()
	ctx := context.Background()
	var managerID uuid.UUID
	if err := e.p.QueryRow(ctx, `INSERT INTO users (username, display_name, password_hash, role) VALUES ('manager', 'm', 'x', 'user') RETURNING id`).Scan(&managerID); err != nil {
		t.Fatal(err)
	}
	var managerRole, botRole uuid.UUID
	if err := e.p.QueryRow(ctx, `INSERT INTO roles (name, position, permissions) VALUES ('managers', 2, $1) RETURNING id`, int64(perm.ManageBots|perm.SendMessages)).Scan(&managerRole); err != nil {
		t.Fatal(err)
	}
	// The bot holds an elevated role, below the manager's: the hierarchy lets the
	// manager see the bot, the acting rule does not let them act as it.
	if err := e.p.QueryRow(ctx, `INSERT INTO roles (name, position, permissions) VALUES ('elevated bot', 1, $1) RETURNING id`, int64(perm.Administrator)).Scan(&botRole); err != nil {
		t.Fatal(err)
	}
	if _, err := e.p.Exec(ctx, `INSERT INTO user_roles (user_id, role_id) VALUES ($1, $2)`, managerID, managerRole); err != nil {
		t.Fatal(err)
	}
	f := e.newBot()
	if _, err := e.p.Exec(ctx, `DELETE FROM user_roles WHERE user_id = $1`, f.bot.UserID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.p.Exec(ctx, `INSERT INTO user_roles (user_id, role_id) VALUES ($1, $2)`, f.bot.UserID, botRole); err != nil {
		t.Fatal(err)
	}
	return &auth.User{ID: managerID, Role: "user"}, f
}

func verifyMountedRoutesMatchGuards(t *testing.T, h *Handler) {
	t.Helper()
	mounted := chi.NewRouter()
	mounted.Route("/admin", func(a chi.Router) { h.MountAdmin(a) })
	var got []string
	if err := chi.Walk(mounted, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		got = append(got, method+" "+strings.ReplaceAll(route, "/*", ""))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var want []string
	for k := range routeGuards {
		want = append(want, k)
	}
	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("mounted routes and the guard table differ:\nmounted:\n%s\ntable:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func verifyRouteGuardResponse(t *testing.T, route, guard string, w *httptest.ResponseRecorder) {
	t.Helper()
	switch guard {
	case "acting":
		if w.Code != http.StatusForbidden {
			t.Errorf("%s must be guarded by loadActing: %d %s", route, w.Code, w.Body)
			return
		}
		if !strings.Contains(w.Body.String(), "only the Owner can manage a bot with administrator") {
			t.Errorf("%s missing administrator guard error: %s", route, w.Body)
		}
	default:
		if w.Code >= 400 {
			t.Errorf("%s (%s) must stay open to the manager: %d %s", route, guard, w.Code, w.Body)
		}
	}
}

func executeGuardedRoute(h *Handler, manager *auth.User, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r := chi.NewRouter()
	r.Route("/admin", func(a chi.Router) {
		a.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				next.ServeHTTP(w, r.WithContext(auth.WithUser(r.Context(), manager)))
			})
		})
		h.MountAdmin(a)
	})
	r.ServeHTTP(w, req)
	return w
}

func verifyRouteGuards(t *testing.T, h *Handler, manager *auth.User, f fixtureBot) {
	t.Helper()
	later := "2099-01-01T00:00:00Z"
	bodies := map[string]string{
		"PATCH /admin/bots/{id}":                      `{"name":"renamed"}`,
		"PUT /admin/bots/{id}/roles":                  `{"role_ids":[]}`,
		"POST /admin/bots/{id}/tokens":                `{"expires_at":"` + later + `"}`,
		"PATCH /admin/bots/{id}/commands/{commandID}": `{"disabled":true}`,
		"POST /admin/bots":                            `{"name":"fresh"}`,
	}
	replacer := strings.NewReplacer("{id}", f.bot.ID.String(), "{tokenID}", f.token.ID.String(), "{commandID}", f.cmd.ID.String())
	for route, guard := range routeGuards {
		method, pattern, _ := strings.Cut(route, " ")
		path := replacer.Replace(pattern)
		w := executeGuardedRoute(h, manager, method, path, bodies[route])
		verifyRouteGuardResponse(t, route, guard, w)
	}
}

func TestEveryManagementRouteHasItsGuard(t *testing.T) {
	e := newEnv(t)
	manager, f := seedManagerAndElevatedBot(t, e)
	h := e.handler(e.p)
	verifyMountedRoutesMatchGuards(t, h)
	verifyRouteGuards(t, h, manager, f)
}
