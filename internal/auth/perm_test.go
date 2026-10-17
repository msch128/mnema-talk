package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/perm"
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
	f := perm.NewFixture()
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
		{"unloadable state", RequirePerm(perm.NewService(nil), perm.ManageRoles), moderator, 500},
	} {
		if got := serve(c.mw, c.user).Code; got != c.status {
			t.Errorf("%s: %d, want %d", c.what, got, c.status)
		}
	}
}

func TestAuthorizeTargetFollowsTheHierarchy(t *testing.T) {
	f := perm.NewFixture()
	high, low := f.Role(5, perm.ManageMembers), f.Role(2, 0)
	boss, peer, junior, plain := &User{ID: uuid.New()}, &User{ID: uuid.New()}, &User{ID: uuid.New()}, &User{ID: uuid.New()}
	f.Assign(boss.ID, high).Assign(peer.ID, high).Assign(junior.ID, low)
	owner := &User{ID: uuid.New(), Role: RoleAdmin}
	h := &Handler{Perms: f.Service()}
	req := func(actor *User) *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/", nil)
		return r.WithContext(WithUser(r.Context(), actor))
	}
	for _, c := range []struct {
		what   string
		actor  *User
		target *User
		ok     bool
	}{
		{"boss over junior", boss, junior, true},
		{"boss over a role-less member", boss, plain, true},
		{"boss over a peer", boss, peer, false},
		{"junior over boss", junior, boss, false},
		{"the Owner over anyone", owner, boss, true},
		{"anyone over the Owner", boss, owner, false},
		{"the Owner over themselves", owner, owner, false},
	} {
		if err := h.authorizeTarget(req(c.actor), c.target); (err == nil) != c.ok {
			t.Errorf("%s: %v", c.what, err)
		}
	}
	// Without a permission service only the Owner reaches this far: the Owner
	// stays untouchable, everyone else is not checked again.
	bare := &Handler{}
	if err := bare.authorizeTarget(req(owner), owner); err == nil {
		t.Error("the Owner is never a target")
	}
	if err := bare.authorizeTarget(req(owner), junior); err != nil {
		t.Errorf("no service: %v", err)
	}
	// An unreadable state fails the check.
	broken := &Handler{Perms: perm.NewService(nil)}
	if err := broken.authorizeTarget(req(boss), junior); err == nil {
		t.Error("an unreadable permission state must not allow the action")
	}
}

func TestWithRoles(t *testing.T) {
	f := perm.NewFixture()
	role := f.Role(1, 0)
	a, b := uuid.New(), uuid.New()
	f.Assign(a, role)
	users := []AdminUser{{User: User{ID: a}}, {User: User{ID: b}}}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	if err := (&Handler{}).withRoles(r, users); err != nil || users[0].RoleIDs != nil {
		t.Fatalf("no service: %v %v", err, users)
	}
	if err := (&Handler{Perms: f.Service()}).withRoles(r, users); err != nil || len(users[0].RoleIDs) != 1 || users[1].RoleIDs != nil {
		t.Fatalf("roles: %v %+v", err, users)
	}
	if err := (&Handler{Perms: perm.NewService(nil)}).withRoles(r, users); err == nil {
		t.Fatal("an unreadable state must fail")
	}
}
