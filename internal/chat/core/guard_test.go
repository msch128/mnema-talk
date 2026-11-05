package core

import (
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// A Guard without a permission service (unit tests, tools) imposes no limit.
func TestGuardWithoutPermissionsAllowsEverything(t *testing.T) {
	g := Guard{}
	r := httptest.NewRequest("GET", "/", nil)
	if err := g.Require(r, uuid.New(), perm.SendMessages); err != nil {
		t.Errorf("Require: %v", err)
	}
	if err := g.RequireCategory(r, uuid.New()); err != nil {
		t.Errorf("RequireCategory: %v", err)
	}
	if ids, err := g.ViewableChannels(r); ids != nil || err != nil {
		t.Errorf("ViewableChannels = %v, %v; want nil, nil (all)", ids, err)
	}
	if ids, err := g.ViewableCategories(r); ids != nil || err != nil {
		t.Errorf("ViewableCategories = %v, %v; want nil, nil (all)", ids, err)
	}
	refusal := httpx.ErrForbidden("no")
	if got := g.RefusalFor(r, refusal, perm.SendMessages); !errors.Is(got, refusal) {
		t.Errorf("RefusalFor changed the refusal to %v", got)
	}
}
