package memberadmin

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/auth/authn"
	"github.com/msch128/mnema-talk/internal/perm/permtest"
)

func TestWithRoles(t *testing.T) {
	f := permtest.NewFixture()
	role := f.Role(1, 0)
	a, b := uuid.New(), uuid.New()
	f.Assign(a, role)
	users := []auth.AdminUser{{User: auth.User{ID: a}}, {User: auth.User{ID: b}}}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	if err := New(&authn.Handler{}).withRoles(r, users); err == nil {
		t.Fatal("a handler without a permission service must fail")
	}
	if err := New(&authn.Handler{Perms: f.Service()}).withRoles(r, users); err != nil || len(users[0].RoleIDs) != 1 || users[1].RoleIDs != nil {
		t.Fatalf("roles: %v %+v", err, users)
	}
	if err := New(&authn.Handler{Perms: permtest.Unloadable()}).withRoles(r, users); err == nil {
		t.Fatal("an unreadable state must fail")
	}
}
