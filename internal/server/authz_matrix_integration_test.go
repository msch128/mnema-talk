//go:build integration

package server

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/bot"
	"github.com/msch128/mnema-talk/internal/perm"
)

// This file is the route-by-role matrix of the HTTP API. It walks the real
// router, so a route added without a guard fails here instead of shipping open:
//   - without a session every route answers 401, except the public ones named
//     in publicRoutes;
//   - a member without any role gets 403 from every /api/admin route;
//   - which single permission opens which admin route is pinned in adminMatrix.

var routeParam = regexp.MustCompile(`\{[^}]+\}`)

// routeSample fills a route's parameters with ids that match nothing, so a
// guard decides before any object is looked at.
func routeSample(route string) string {
	return routeParam.ReplaceAllStringFunc(route, func(p string) string {
		switch p {
		case "{slug}", "{token}", "{code}", "{emoji}":
			return "x"
		case "{number}", "{rev}":
			return "1"
		}
		return uuid.NewString()
	})
}

type apiRoute struct{ method, pattern string }

func (r apiRoute) String() string { return r.method + " " + r.pattern }

// body is what a probe sends: nothing for reads and deletes, an empty object
// otherwise.
func (r apiRoute) body() any {
	if r.method == http.MethodGet || r.method == http.MethodDelete {
		return nil
	}
	return map[string]any{}
}

// apiRoutes lists the /api routes of the router (the catch-all web app and the
// WebSocket upgrade excluded).
func apiRoutes(t *testing.T, a *app) []apiRoute {
	t.Helper()
	mux, ok := a.router.Handler.(chi.Routes)
	if !ok {
		t.Fatal("router is not a chi mux")
	}
	var out []apiRoute
	err := chi.Walk(mux, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if strings.HasPrefix(route, "/api/") && !strings.HasSuffix(route, "/*") && route != "/api/ws" {
			out = append(out, apiRoute{method, route})
		}
		return nil
	})
	if err != nil || len(out) < 250 {
		t.Fatalf("walked %d routes: %v", len(out), err)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out
}

// publicRoutes are the routes that work without a session. Each carries its own
// secret or reveals nothing that is not public. The bot API takes a bot token
// and is covered by the bot tests.
var publicRoutes = map[string]bool{
	"GET /api/health":                       true,
	"GET /api/health/live":                  true,
	"GET /api/legal":                        true,
	"GET /api/community/icon":               true,
	"GET /api/auth/options":                 true,
	"POST /api/auth/login":                  true,
	"POST /api/auth/login/2fa":              true,
	"POST /api/auth/logout":                 true,
	"POST /api/auth/register":               true,
	"POST /api/auth/password-reset/request": true,
	"POST /api/auth/password-reset/confirm": true,
	"POST /api/auth/email/verify":           true,
	"GET /api/invites/{code}":               true,
	"GET /api/join/{slug}":                  true,
	"POST /api/webhooks/{id}/{token}":       true,
}

func TestEveryRouteNeedsASessionUnlessItIsPublic(t *testing.T) {
	a := newApp(t, false)
	anon := a.anon()
	seen := map[string]bool{}
	for _, r := range apiRoutes(t, a) {
		seen[r.String()] = true
		if strings.HasPrefix(r.pattern, "/api/bot/v1/") {
			continue
		}
		if publicRoutes[r.String()] {
			continue
		}
		res := anon.do(r.method, routeSample(r.pattern), r.body(), nil)
		if res.status != http.StatusUnauthorized {
			t.Errorf("%s without a session: %d %s, want 401", r, res.status, res.body)
		}
	}
	for route := range publicRoutes {
		if !seen[route] {
			t.Errorf("publicRoutes names %s, which is not mounted", route)
		}
	}
}

func TestAMemberWithoutRolesIsRefusedEveryAdminRoute(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	member := a.quickMember(admin, "plain")
	for _, r := range apiRoutes(t, a) {
		if !strings.HasPrefix(r.pattern, "/api/admin/") {
			continue
		}
		if res := member.do(r.method, routeSample(r.pattern), r.body(), nil); res.status != http.StatusForbidden {
			t.Errorf("%s as a plain member: %d %s, want 403", r, res.status, res.body)
		}
	}
}

// quickMember adds a member without going through the sign-up route, whose
// per-address limit a test that needs many accounts would trip.
func (a *app) quickMember(admin *client, name string) *client {
	a.t.Helper()
	inv := admin.post("/api/admin/invites", map[string]any{})
	if inv.status != http.StatusCreated {
		a.t.Fatalf("create invite: %d %s", inv.status, inv.body)
	}
	var invite auth.Invite
	inv.decode(a.t, &invite)
	if _, err := auth.Register(context.Background(), a.db, name, name, "member-password-123", invite.Code); err != nil {
		a.t.Fatal(err)
	}
	return a.login(name, "member-password-123")
}

// adminMatrix says which single permission lets a member past each admin
// route's guard (an answer other than 403 means the guard let them through).
// "Admin" is the Administrator permission; the routes only the Owner passes
// list nobody. It follows doc/moderation.md and the permission docs; change a
// row only when the rule itself changes.
var adminMatrix = map[string]string{
	"GET /api/admin/audit-log":                               "Admin,Audit",
	"GET /api/admin/automod":                                 "Admin",
	"POST /api/admin/automod/rules":                          "Admin",
	"DELETE /api/admin/automod/rules/{id}":                   "Admin",
	"PUT /api/admin/automod/rules/{id}":                      "Admin",
	"GET /api/admin/bans":                                    "Admin,Ban",
	"GET /api/admin/bots":                                    "Admin,Bots",
	"POST /api/admin/bots":                                   "Admin,Bots",
	"POST /api/admin/bots/install":                           "Admin,Bots",
	"POST /api/admin/bots/manifests/check":                   "Admin,Bots",
	"DELETE /api/admin/bots/{id}":                            "Admin,Bots",
	"GET /api/admin/bots/{id}":                               "Admin,Bots",
	"PATCH /api/admin/bots/{id}":                             "Admin,Bots",
	"GET /api/admin/bots/{id}/access":                        "Admin,Bots",
	"POST /api/admin/bots/{id}/avatar":                       "Admin,Bots",
	"GET /api/admin/bots/{id}/commands":                      "Admin,Bots",
	"DELETE /api/admin/bots/{id}/commands/{commandID}":       "Admin,Bots",
	"PATCH /api/admin/bots/{id}/commands/{commandID}":        "Admin,Bots",
	"PUT /api/admin/bots/{id}/roles":                         "Admin,Bots",
	"GET /api/admin/bots/{id}/tokens":                        "Admin,Bots",
	"POST /api/admin/bots/{id}/tokens":                       "Admin,Bots",
	"DELETE /api/admin/bots/{id}/tokens/{tokenID}":           "Admin,Bots",
	"POST /api/admin/bots/{id}/tokens/{tokenID}/regenerate":  "Admin,Bots",
	"POST /api/admin/categories":                             "Admin,Chan",
	"DELETE /api/admin/categories/{id}":                      "Admin,Chan",
	"PATCH /api/admin/categories/{id}":                       "Admin,Chan",
	"GET /api/admin/categories/{id}/overwrites":              "Admin,Chan",
	"PUT /api/admin/categories/{id}/overwrites":              "Admin,Chan",
	"POST /api/admin/channels":                               "Admin,Chan",
	"DELETE /api/admin/channels/{id}":                        "Admin,Chan",
	"PATCH /api/admin/channels/{id}":                         "Admin,Chan",
	"PUT /api/admin/channels/{id}/auto-delete":               "Admin,Chan",
	"POST /api/admin/channels/{id}/auto-delete/preview":      "Admin,Chan",
	"GET /api/admin/message-settings":                        "Admin",
	"PUT /api/admin/message-settings/lifetimes":              "Admin",
	"PUT /api/admin/message-settings/auto-delete":            "Admin",
	"POST /api/admin/message-settings/auto-delete/preview":   "Admin",
	"POST /api/admin/channels/{id}/duplicate":                "Admin,Chan",
	"GET /api/admin/channels/{id}/outgoing-webhooks":         "Admin",
	"POST /api/admin/channels/{id}/outgoing-webhooks":        "Admin",
	"GET /api/admin/channels/{id}/overwrites":                "Admin,Chan",
	"PUT /api/admin/channels/{id}/overwrites":                "Admin,Chan",
	"GET /api/admin/channels/{id}/webhooks":                  "Admin",
	"POST /api/admin/channels/{id}/webhooks":                 "Admin",
	"DELETE /api/admin/community/icon":                       "Admin",
	"PUT /api/admin/community/icon":                          "Admin",
	"POST /api/admin/emoji":                                  "Admin,Emoji",
	"DELETE /api/admin/emoji/{id}":                           "Admin,Emoji",
	"PATCH /api/admin/emoji/{id}":                            "Admin,Emoji",
	"GET /api/admin/first-run":                               "",
	"PUT /api/admin/first-run":                               "",
	"POST /api/admin/import/discord/apply":                   "Admin",
	"GET /api/admin/import/discord/imports":                  "Admin",
	"GET /api/admin/import/discord/imports/{id}":             "Admin",
	"POST /api/admin/import/discord/imports/{id}/undo":       "Admin",
	"POST /api/admin/import/discord/preview":                 "Admin",
	"GET /api/admin/invites":                                 "Admin,Inv",
	"POST /api/admin/invites":                                "Admin,Inv",
	"DELETE /api/admin/invites/{id}":                         "Admin,Inv",
	"PATCH /api/admin/invites/{id}":                          "Admin,Inv",
	"PUT /api/admin/layout":                                  "Admin,Chan",
	"GET /api/admin/media":                                   "",
	"GET /api/admin/media/orphans":                           "",
	"POST /api/admin/media/orphans/cleanup":                  "",
	"POST /api/admin/media/prune":                            "",
	"GET /api/admin/media/stats":                             "",
	"DELETE /api/admin/media/{id}":                           "",
	"GET /api/admin/onboarding":                              "Admin",
	"PUT /api/admin/onboarding":                              "Admin",
	"DELETE /api/admin/outgoing-webhooks/{id}":               "Admin",
	"PATCH /api/admin/outgoing-webhooks/{id}":                "Admin",
	"GET /api/admin/outgoing-webhooks/{id}/deliveries":       "Admin",
	"POST /api/admin/outgoing-webhooks/{id}/secret":          "Admin",
	"POST /api/admin/outgoing-webhooks/{id}/test":            "Admin",
	"GET /api/admin/raid":                                    "Admin",
	"PUT /api/admin/raid":                                    "Admin",
	"PUT /api/admin/raid/lockdown":                           "Admin",
	"GET /api/admin/reports":                                 "Admin,Rep",
	"GET /api/admin/reports/open-count":                      "Admin,Rep",
	"DELETE /api/admin/reports/{id}":                         "Admin,Rep",
	"GET /api/admin/reports/{id}":                            "Admin,Rep",
	"POST /api/admin/reports/{id}/resolve":                   "Admin,Rep",
	"POST /api/admin/roles":                                  "Admin,Roles",
	"PUT /api/admin/roles/order":                             "Admin,Roles",
	"DELETE /api/admin/roles/{id}":                           "Admin,Roles",
	"PATCH /api/admin/roles/{id}":                            "Admin,Roles",
	"GET /api/admin/roles/{id}/view-as":                      "Admin,Roles",
	"GET /api/admin/status":                                  "Admin",
	"GET /api/admin/status/capacity":                         "Admin",
	"POST /api/admin/status/websocket":                       "Admin",
	"GET /api/admin/system":                                  "",
	"POST /api/admin/system/check":                           "",
	"POST /api/admin/system/mail/test":                       "",
	"GET /api/admin/system/ops":                              "",
	"GET /api/admin/system/update":                           "",
	"POST /api/admin/system/update":                          "",
	"GET /api/admin/upload-limit":                            "",
	"PUT /api/admin/upload-limit":                            "",
	"GET /api/admin/users":                                   "Admin,Roles,Memb",
	"DELETE /api/admin/users/{id}/ban":                       "Admin,Ban",
	"POST /api/admin/users/{id}/ban":                         "Admin,Ban",
	"POST /api/admin/users/{id}/disable":                     "Admin,Memb",
	"POST /api/admin/users/{id}/enable":                      "Admin,Memb",
	"POST /api/admin/users/{id}/kick":                        "Admin,Disc",
	"POST /api/admin/users/{id}/login-lockout/clear":         "",
	"GET /api/admin/users/{id}/moderation":                   "Admin,Rep",
	"POST /api/admin/users/{id}/moderation/notes":            "Admin,Rep",
	"DELETE /api/admin/users/{id}/moderation/notes/{noteID}": "Admin,Rep",
	"PUT /api/admin/users/{id}/moderation/notes/{noteID}":    "Admin,Rep",
	"POST /api/admin/users/{id}/password":                    "",
	"POST /api/admin/users/{id}/password-reset":              "",
	"PUT /api/admin/users/{id}/roles":                        "Admin,Roles",
	"POST /api/admin/users/{id}/sessions/revoke":             "Admin,Memb",
	"PUT /api/admin/users/{id}/status":                       "Admin,Memb",
	"DELETE /api/admin/users/{id}/timeout":                   "Admin,Tmo",
	"POST /api/admin/users/{id}/timeout":                     "Admin,Tmo",
	"POST /api/admin/users/{id}/two-factor/reset":            "",
	"GET /api/admin/vanity-link":                             "Admin",
	"PUT /api/admin/vanity-link":                             "Admin",
	"DELETE /api/admin/webhooks/{id}":                        "Admin",
	"PATCH /api/admin/webhooks/{id}":                         "Admin",
	"POST /api/admin/webhooks/{id}/avatar":                   "Admin",
	"POST /api/admin/webhooks/{id}/token":                    "Admin",
}

var matrixPerms = []struct {
	name string
	set  perm.Set
}{
	{"Admin", perm.Administrator}, {"Chan", perm.ManageChannels}, {"Roles", perm.ManageRoles},
	{"Inv", perm.CreateInvites}, {"Hook", perm.ManageWebhooks}, {"Memb", perm.ManageMembers},
	{"Disc", perm.DisconnectMembers}, {"Audit", perm.ViewAuditLog}, {"Tmo", perm.TimeoutMembers},
	{"Ban", perm.BanMembers}, {"Bots", perm.ManageBots}, {"Emoji", perm.ManageEmoji},
	{"Rep", perm.ManageReports}, {"Kick", perm.KickMembers},
	// Not part of the admin area: they must open no admin route.
	{"Msgs", perm.ManageMessages}, {"Evt", perm.ManageEvents}, {"Wiki", perm.ManageWiki}, {"Stage", perm.ManageStage},
}

func TestAdminRoutesOpenOnlyForTheirPermission(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	holders := make([]*client, len(matrixPerms))
	for i, p := range matrixPerms {
		holders[i] = a.quickMember(admin, fmt.Sprintf("holder%d", i))
		assignRoles(t, admin, holders[i], createRole(t, admin, p.name, p.set))
	}
	seen := map[string]bool{}
	for _, r := range apiRoutes(t, a) {
		if !strings.HasPrefix(r.pattern, "/api/admin/") {
			continue
		}
		seen[r.String()] = true
		want, known := adminMatrix[r.String()]
		if !known {
			t.Errorf("%s is not in adminMatrix: decide which permission opens it", r)
			continue
		}
		var opens []string
		for i, p := range matrixPerms {
			if res := holders[i].do(r.method, routeSample(r.pattern), r.body(), nil); res.status != http.StatusForbidden && res.status != http.StatusUnauthorized {
				opens = append(opens, p.name)
			}
		}
		if got := strings.Join(opens, ","); got != want {
			t.Errorf("%s is opened by [%s], want [%s]", r, got, want)
		}
	}
	for route := range adminMatrix {
		if !seen[route] {
			t.Errorf("adminMatrix names %s, which is not mounted", route)
		}
	}
}

// TestCredentialsDoNotCrossTheirAPIs: a bot token opens nothing of the member
// API (not even with a cookie-less call that passes the origin check), and a
// member's cookie or a webhook secret opens nothing of the bot API.
func TestCredentialsDoNotCrossTheirAPIs(t *testing.T) {
	f := newBotFixture(t, false)
	member := f.a.quickMember(f.owner, "someone")
	var hook struct {
		ID    uuid.UUID `json:"id"`
		Token string    `json:"token"`
	}
	res := f.owner.post("/api/admin/channels/"+f.open.String()+"/webhooks", map[string]any{"name": "hook"})
	wantStatus(t, "webhook", res, http.StatusCreated)
	res.decode(t, &hook)
	if hook.Token == "" {
		t.Fatalf("no webhook token: %s", res.body)
	}

	for _, p := range []struct{ method, path string }{
		{"GET", "/api/auth/me"},
		{"GET", "/api/channels"},
		{"GET", "/api/channels/" + f.open.String() + "/messages"},
		{"POST", "/api/channels/" + f.open.String() + "/messages"},
		{"GET", "/api/admin/bots"},
		{"GET", "/api/admin/users"},
		{"POST", "/api/admin/invites"},
	} {
		for who, hdr := range map[string]map[string]string{
			"bot token":      {"Authorization": "Bot " + f.token.Secret},
			"bearer token":   {"Authorization": "Bearer " + f.token.Secret},
			"webhook secret": {"Authorization": "Bot " + hook.Token},
		} {
			res := f.a.anon().do(p.method, p.path, map[string]any{"content": "x"}, hdr)
			if res.status != http.StatusUnauthorized {
				t.Errorf("%s on %s %s: %d %s, want 401", who, p.method, p.path, res.status, res.body)
			}
		}
	}

	botRoutes := []string{
		bot.BasePath + "/users/@me",
		botPath(f.open, "/messages"),
		bot.BasePath + "/channels",
		bot.BasePath + "/members",
	}
	for _, path := range botRoutes {
		for who, hdr := range map[string]map[string]string{
			"no credential":  nil,
			"webhook secret": {"Authorization": "Bot " + hook.Token},
			"owner as bot":   {"Authorization": "Bot " + "x"},
		} {
			if res := (&botClient{t: t, a: f.a}).req("GET", path, nil, hdr); res.status != http.StatusUnauthorized && res.status != http.StatusTooManyRequests {
				t.Errorf("%s on GET %s: %d %s, want 401", who, path, res.status, res.body)
			}
		}
		// A member's session cookie is not read by the bot API.
		req, _ := http.NewRequest("GET", f.a.srv.URL+path, nil)
		for _, ck := range member.http.Jar.Cookies(mustURL(f.a.srv.URL)) {
			req.AddCookie(ck)
		}
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		if r.StatusCode != http.StatusUnauthorized && r.StatusCode != http.StatusTooManyRequests {
			t.Errorf("a member's cookie on GET %s: %d, want 401", path, r.StatusCode)
		}
	}
}
