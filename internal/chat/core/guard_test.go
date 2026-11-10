package core

import (
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/perm/permtest"
)

// A Guard without a permission service answers no question with "allowed",
// not even for the Owner.
func TestGuardWithoutPermissionsRefusesEverything(t *testing.T) {
	g := Guard{}
	owner := &auth.User{ID: uuid.New(), Role: "admin"}
	r := httptest.NewRequest("GET", "/", nil)
	r = r.WithContext(auth.WithUser(r.Context(), owner))
	if err := g.Require(r, uuid.New(), perm.SendMessages); err == nil {
		t.Error("Require let the caller through")
	}
	if p, err := g.ChannelPerms(r, uuid.New()); err == nil || p != 0 {
		t.Errorf("ChannelPerms = %v, %v", p, err)
	}
	if err := g.RequireCategory(r, uuid.New()); err == nil {
		t.Error("RequireCategory let the caller through")
	}
	if ids, err := g.ViewableChannels(r); err == nil {
		t.Errorf("ViewableChannels = %v, nil: a nil list means every channel", ids)
	}
	if ids, err := g.ViewableCategories(r); err == nil {
		t.Errorf("ViewableCategories = %v, nil", ids)
	}
	refusal := httpx.ErrForbidden("no")
	if got := g.RefusalFor(r, refusal, perm.SendMessages); !errors.Is(got, refusal) {
		t.Errorf("RefusalFor changed the refusal to %v", got)
	}
}

func TestNewGuardPanicsWithoutAPermissionService(t *testing.T) {
	if svc := permtest.NewFixture().Service(); NewGuard(svc).Perms != svc {
		t.Fatal("NewGuard dropped the service")
	}
	defer func() {
		if recover() == nil {
			t.Fatal("built a Guard without a permission service")
		}
	}()
	NewGuard(nil)
}
