package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/perm/permtest"
)

func serve(mw func(http.Handler) http.Handler, user *User) *httptest.ResponseRecorder {
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	if user != nil {
		r = r.WithContext(WithUser(r.Context(), user))
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestRequireOwnerAdmitsOnlyTheOwner(t *testing.T) {
	owner, member := &User{ID: uuid.New(), Role: RoleAdmin}, &User{ID: uuid.New(), Role: RoleUser}
	for _, c := range []struct {
		user   *User
		status int
	}{{nil, 403}, {member, 403}, {owner, 204}} {
		if got := serve(RequireOwner, c.user).Code; got != c.status {
			t.Errorf("user %v: %d, want %d", c.user, got, c.status)
		}
	}
}

func TestPermissionGuards(t *testing.T) {
	f := permtest.NewFixture()
	mod := f.Role(1, perm.ManageRoles)
	moderator, plain := &User{ID: uuid.New(), Role: RoleUser}, &User{ID: uuid.New(), Role: RoleUser}
	owner := &User{ID: uuid.New(), Role: RoleAdmin}
	f.Assign(moderator.ID, mod)
	svc := f.Service()

	need := RequirePerm(svc, perm.ManageRoles)
	any := RequireAnyPerm(svc, perm.ManageRoles|perm.CreateInvites)
	for _, c := range []struct {
		what   string
		mw     func(http.Handler) http.Handler
		user   *User
		status int
	}{
		{"role holder", need, moderator, 204},
		{"member", need, plain, 403},
		{"owner", need, owner, 204},
		{"nobody", need, nil, 401},
		{"any: holder", any, moderator, 204},
		{"any: member", any, plain, 403},
		{"any: owner", any, owner, 204},
		{"without a service only the owner", RequirePerm(nil, perm.ManageRoles), moderator, 403},
		{"without a service the owner", RequireAnyPerm(nil, perm.ManageRoles), owner, 204},
		// A permission state that cannot be loaded is a server failure, not a pass.
		{"unloadable state", RequirePerm(permtest.Unloadable(), perm.ManageRoles), moderator, 500},
	} {
		if got := serve(c.mw, c.user).Code; got != c.status {
			t.Errorf("%s: %d, want %d", c.what, got, c.status)
		}
	}
}
