package authn

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/perm/permtest"
)

func TestCheckTargetFollowsTheHierarchy(t *testing.T) {
	f := permtest.NewFixture()
	high, low := f.Role(5, perm.ManageMembers), f.Role(2, 0)
	boss, peer, junior, plain := &auth.User{ID: uuid.New()}, &auth.User{ID: uuid.New()}, &auth.User{ID: uuid.New()}, &auth.User{ID: uuid.New()}
	f.Assign(boss.ID, high).Assign(peer.ID, high).Assign(junior.ID, low)
	owner := &auth.User{ID: uuid.New(), Role: auth.RoleAdmin}
	snap, err := f.Service().Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		what   string
		actor  *auth.User
		target *auth.User
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
		if err := checkTarget(snap, c.actor, c.target); (err == nil) != c.ok {
			t.Errorf("%s: %v", c.what, err)
		}
	}
	// Without a permission service only the Owner reaches this far: the Owner
	// stays untouchable, everyone else is not checked again.
	if err := checkTarget(nil, owner, owner); err == nil {
		t.Error("the Owner is never a target")
	}
	if err := checkTarget(nil, owner, junior); err != nil {
		t.Errorf("no service: %v", err)
	}
	// No acting member never passes the check.
	if err := checkTarget(snap, nil, junior); err == nil {
		t.Error("a missing actor must not be allowed to act")
	}
}
