package perm_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	. "github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/perm/permtest"
)

func TestAsRoleOnlyResolvesLikeAMemberWithJustThatRole(t *testing.T) {
	f := permtest.NewFixture()
	reader := f.Role(1, ViewChannel)
	other := f.Role(2, SendMessages)
	ch := f.Channel(uuid.Nil)
	f.RoleOverwrite(true, ch, f.EveryoneID(), 0, ViewChannel)
	f.RoleOverwrite(true, ch, reader, ViewChannel, 0)

	// Real members: one with both roles, one timed out and one who is the Owner.
	real, owner := uuid.New(), uuid.New()
	f.Assign(real, reader).Assign(real, other).Owner(owner)
	f.Timeout(real, time.Now().Add(time.Hour), "")
	f.Rules(3)

	view, sub, ok := f.Snapshot().AsRoleOnly(reader)
	if !ok {
		t.Fatal("role not found")
	}
	if sub.Owner || view.IsOwner(sub) {
		t.Fatal("the preview member is never the Owner")
	}
	got := view.Channel(sub, ch)
	if !got.Has(ViewChannel) || !got.Has(SendMessages) {
		t.Errorf("@everyone's send right plus the role's view overwrite expected, got %b", got)
	}
	if view.IsOwner(Subject{ID: owner}) {
		t.Error("the copy must not carry the real Owner")
	}
	if f.Snapshot().Channel(Subject{ID: real}, ch).Has(SendMessages) {
		t.Error("the real timed-out member must be unaffected by the preview")
	}
	if _, _, ok := f.Snapshot().AsRoleOnly(uuid.New()); ok {
		t.Error("an unknown role must not resolve")
	}
}

func TestAsRoleOnlyForEveryoneHoldsNoCustomRole(t *testing.T) {
	f := permtest.NewFixture()
	role := f.Role(1, ManageRoles)
	view, sub, ok := f.Snapshot().AsRoleOnly(f.EveryoneID())
	if !ok || view.Global(sub).Has(ManageRoles) {
		t.Fatal("@everyone alone must not carry a custom role's rights")
	}
	view, sub, _ = f.Snapshot().AsRoleOnly(role)
	if !view.Global(sub).Has(ManageRoles) {
		t.Fatal("the role's rights are missing")
	}
}

func TestAsRoleOnlyPreviewsAManagedRoleAsABotWithoutEveryone(t *testing.T) {
	f := permtest.NewFixture().Everyone(SendMessages)
	role := f.ManagedRole(1, ViewChannel)

	view, sub, ok := f.Snapshot().AsRoleOnly(role)
	if !ok {
		t.Fatal("role not found")
	}
	if got := view.Global(sub); got.Has(SendMessages) || !got.Has(ViewChannel) {
		t.Errorf("a managed role is held by a bot, which does not inherit @everyone, got %b", got)
	}
}
