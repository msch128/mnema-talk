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
