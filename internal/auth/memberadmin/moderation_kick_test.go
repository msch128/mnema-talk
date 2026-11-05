package memberadmin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/msch128/mnema-talk/internal/auth"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/perm/permtest"
)

func requestAs(u *auth.User) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	if u == nil {
		return r
	}
	return r.WithContext(auth.WithUser(context.Background(), u))
}

func TestRequirePermissionAdmitsOnlyWhoHoldsIt(t *testing.T) {
	owner := &auth.User{ID: uuid.New(), Role: auth.RoleAdmin}
	member := &auth.User{ID: uuid.New(), Role: auth.RoleUser}
	if err := New(&auth.Handler{}).RequirePermission(requestAs(nil), perm.KickMembers); err == nil {
		t.Error("no session passed")
	}
	// Without a permission service only the Owner passes.
	if err := New(&auth.Handler{}).RequirePermission(requestAs(owner), perm.KickMembers); err != nil {
		t.Errorf("Owner: %v", err)
	}
	if err := New(&auth.Handler{}).RequirePermission(requestAs(member), perm.KickMembers); err == nil {
		t.Error("member passed without a permission service")
	}
	f := permtest.NewFixture()
	holder := uuid.New()
	f.Assign(holder, f.Role(1, perm.KickMembers))
	h := New(&auth.Handler{Perms: f.Service()})
	if err := h.RequirePermission(requestAs(&auth.User{ID: holder, Role: auth.RoleUser}), perm.KickMembers); err != nil {
		t.Errorf("holder: %v", err)
	}
	err := h.RequirePermission(requestAs(member), perm.KickMembers)
	if apiErr, ok := httpx.AsAPIError(err); !ok || apiErr.Status != http.StatusForbidden {
		t.Errorf("member without the permission: %v", err)
	}
}

func TestRequirePermissionSanitizesAnUnreadablePermissionState(t *testing.T) {
	h := New(&auth.Handler{Perms: permtest.Unloadable()})
	err := h.RequirePermission(requestAs(&auth.User{ID: uuid.New(), Role: auth.RoleUser}), perm.KickMembers)
	apiErr, ok := httpx.AsAPIError(err)
	if !ok || apiErr.Status != http.StatusInternalServerError {
		t.Fatalf("want a sanitized 500 like the route guard, got %v", err)
	}
}
