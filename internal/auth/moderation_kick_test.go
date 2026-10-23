package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

func requestAs(u *User) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	if u == nil {
		return r
	}
	return r.WithContext(WithUser(context.Background(), u))
}

func TestRequirePermissionAdmitsOnlyWhoHoldsIt(t *testing.T) {
	owner := &User{ID: uuid.New(), Role: RoleAdmin}
	member := &User{ID: uuid.New(), Role: RoleUser}
	if err := (&Handler{}).RequirePermission(requestAs(nil), perm.KickMembers); err == nil {
		t.Error("no session passed")
	}
	// Without a permission service only the Owner passes.
	if err := (&Handler{}).RequirePermission(requestAs(owner), perm.KickMembers); err != nil {
		t.Errorf("Owner: %v", err)
	}
	if err := (&Handler{}).RequirePermission(requestAs(member), perm.KickMembers); err == nil {
		t.Error("member passed without a permission service")
	}
	f := perm.NewFixture()
	holder := uuid.New()
	f.Assign(holder, f.Role(1, perm.KickMembers))
	h := &Handler{Perms: f.Service()}
	if err := h.RequirePermission(requestAs(&User{ID: holder, Role: RoleUser}), perm.KickMembers); err != nil {
		t.Errorf("holder: %v", err)
	}
	err := h.RequirePermission(requestAs(member), perm.KickMembers)
	if apiErr, ok := httpx.AsAPIError(err); !ok || apiErr.Status != http.StatusForbidden {
		t.Errorf("member without the permission: %v", err)
	}
}
