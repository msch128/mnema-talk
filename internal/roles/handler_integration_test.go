//go:build integration

package roles

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/testutil"
)

func TestMain(m *testing.M) { os.Exit(testutil.Main(m)) }

func request(ctx context.Context, user *auth.User, body string, params map[string]string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)).WithContext(ctx)
	route := chi.NewRouteContext()
	for k, v := range params {
		route.URLParams.Add(k, v)
	}
	return r.WithContext(context.WithValue(auth.WithUser(r.Context(), user), chi.RouteCtxKey, route))
}

// Every handler refuses bad input and passes a failing permission store on
// as an error; the rules themselves are tested in package perm and through
// the router in package server.
func TestHandlersReportBadInputAndStoreFailures(t *testing.T) {
	p := testutil.DB(t)
	testutil.Reset(t, p)
	h := &Handler{Perms: perm.NewService(p), Events: &events.Recorder{}}
	owner := &auth.User{ID: uuid.New(), Role: auth.RoleAdmin}
	id := uuid.NewString()

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	handlers := []struct {
		name string
		run  func(http.ResponseWriter, *http.Request) error
		body bool
		id   bool
	}{
		{"permissions", h.myPermissions, false, false},
		{"list roles", h.listRoles, false, false},
		{"list self-assignable roles", h.listSelfAssignable, false, false},
		{"take a role", h.takeSelfRole, false, true},
		{"drop a role", h.dropSelfRole, false, true},
		{"create role", h.createRole, true, false},
		{"update role", h.updateRole, true, true},
		{"delete role", h.deleteRole, false, true},
		{"reorder roles", h.reorderRoles, true, false},
		{"set member roles", h.setMemberRoles, true, true},
		{"channel overwrites", h.getChannelOverwrites, false, true},
		{"set channel overwrites", h.setChannelOverwrites, true, true},
		{"category overwrites", h.getCategoryOverwrites, false, true},
		{"set category overwrites", h.setCategoryOverwrites, true, true},
	}
	for _, c := range handlers {
		t.Run(c.name, func(t *testing.T) {
			params := map[string]string{}
			if c.id {
				params["id"] = id
			}
			if c.body {
				// malformed JSON
				if err := c.run(httptest.NewRecorder(), request(context.Background(), owner, "{", params)); err == nil {
					t.Fatal("malformed body accepted")
				}
			}
			if c.id {
				if err := c.run(httptest.NewRecorder(), request(context.Background(), owner, "{}", map[string]string{"id": "not-a-uuid"})); err == nil {
					t.Fatal("bad id accepted")
				}
			}
			// The store cannot work: the request is already cancelled.
			if err := c.run(httptest.NewRecorder(), request(cancelled, owner, `{"name":"x","role_ids":[],"overwrites":[]}`, params)); err == nil {
				t.Fatal("a failing store must fail the request")
			}
		})
	}
}

func TestMembersChooseOpenRolesThroughTheHandlers(t *testing.T) {
	p := testutil.DB(t)
	testutil.Reset(t, p)
	svc := perm.NewService(p)
	rec := &events.Recorder{}
	var memberID, ownerID uuid.UUID
	for name, dst := range map[string]*uuid.UUID{"owner": &ownerID, "member": &memberID} {
		kind := "user"
		if name == "owner" {
			kind = "admin"
		}
		if err := p.QueryRow(context.Background(), `INSERT INTO users (username, display_name, password_hash, role) VALUES ($1, $1, 'x', $2) RETURNING id`, name, kind).Scan(dst); err != nil {
			t.Fatal(err)
		}
	}
	actor := func(ctx context.Context) (uuid.UUID, string, bool) {
		if u := auth.UserFrom(ctx); u != nil {
			return u.ID, "member", true
		}
		return uuid.Nil, "", false
	}
	h := &Handler{Perms: svc, Events: rec, Audit: &audit.Log{DB: p, Actor: actor}}
	member := &auth.User{ID: memberID}
	name, open, perms := "Gamer", true, perm.SendMessages
	role, err := svc.CreateRole(context.Background(), perm.Subject{ID: ownerID, Owner: true}, perm.RoleInput{Name: &name, Permissions: &perms, SelfAssignable: &open})
	if err != nil {
		t.Fatal(err)
	}
	params := map[string]string{"id": role.ID.String()}

	list := httptest.NewRecorder()
	if err := h.listSelfAssignable(list, request(context.Background(), member, "", nil)); err != nil || !strings.Contains(list.Body.String(), role.ID.String()) {
		t.Fatalf("list: %v %s", err, list.Body)
	}
	for _, step := range []struct {
		run    func(http.ResponseWriter, *http.Request) error
		action string
	}{{h.takeSelfRole, "role.self_assign"}, {h.takeSelfRole, ""}, {h.dropSelfRole, "role.self_remove"}} {
		w := httptest.NewRecorder()
		if err := step.run(w, request(context.Background(), member, "", params)); err != nil || w.Code != http.StatusNoContent {
			t.Fatalf("%s: %v %d", step.action, err, w.Code)
		}
	}
	rows, err := p.Query(context.Background(), `SELECT action, target_name, actor_id FROM audit_log ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var action, target string
		var by *uuid.UUID
		if err := rows.Scan(&action, &target, &by); err != nil {
			t.Fatal(err)
		}
		if target != "Gamer" || by == nil || *by != memberID {
			t.Fatalf("entry %s names %q by %v", action, target, by)
		}
		got = append(got, action)
	}
	if strings.Join(got, ",") != "role.self_assign,role.self_remove" {
		t.Fatalf("audited %v: asking for what one already has leaves nothing", got)
	}
	if len(rec.Events) != 2 || rec.Events[0].Type != events.RolesChanged {
		t.Fatalf("roles_changed broadcast once per real change: %+v", rec.Events)
	}
}
