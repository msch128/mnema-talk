//go:build integration

package perm_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/perm"
)

// A new role lands just above the managed bottom block, but never at or above
// the creator's own highest role: after the Owner moves the Bot role above a
// custom role, that role is the bottom and the new role goes below it.
func TestCreateRoleStaysBelowTheCreatorsTopRole(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	var botUser uuid.UUID
	if err := w.p.QueryRow(ctx, `INSERT INTO users (username, display_name, password_hash, role, is_bot) VALUES ('bot-y', 'y', '!', 'user', TRUE) RETURNING id`).Scan(&botUser); err != nil {
		t.Fatal(err)
	}
	if _, err := w.p.Exec(ctx, `INSERT INTO bots (user_id, name) VALUES ($1, 'y')`, botUser); err != nil {
		t.Fatal(err)
	}
	// A bot's own role also sits at position 1, next to the Bot role.
	if _, err := w.p.Exec(ctx, `INSERT INTO roles (name, position, managed, bot_user_id) VALUES ('y', 1, TRUE, $1)`, botUser); err != nil {
		t.Fatal(err)
	}
	w.svc.Invalidate()

	managers := perm.ManageRoles
	name := "X"
	x, err := w.svc.CreateRole(ctx, w.owner, perm.RoleInput{Name: &name, Permissions: &managers})
	if err != nil {
		t.Fatal(err)
	}
	if x.Position != 2 {
		t.Fatalf("X above the managed block: position %d", x.Position)
	}
	roles, err := w.svc.Roles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var botRole uuid.UUID
	for _, r := range roles {
		if r.Managed && r.BotUserID == nil {
			botRole = r.ID
		}
	}
	// Bot role above X: X now sits at position 1.
	if err := w.svc.ReorderRoles(ctx, w.owner, []uuid.UUID{botRole, x.ID}); err != nil {
		t.Fatal(err)
	}
	if err := w.svc.SetMemberRoles(ctx, w.owner, w.member.ID, []uuid.UUID{x.ID}); err != nil {
		t.Fatal(err)
	}

	pos := func(id uuid.UUID) int {
		t.Helper()
		all, err := w.svc.Roles(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range all {
			if r.ID == id {
				return r.Position
			}
		}
		t.Fatalf("role %s missing", id)
		return 0
	}
	if pos(x.ID) != 1 {
		t.Fatalf("X at %d after the reorder, want 1", pos(x.ID))
	}
	n := "Y"
	y, err := w.svc.CreateRole(ctx, w.member, perm.RoleInput{Name: &n})
	if err != nil {
		if apiCode(err) == 403 {
			return // refused is as good as below
		}
		t.Fatal(err)
	}
	if pos(y.ID) >= pos(x.ID) {
		t.Fatalf("new role at %d, not below X at %d", pos(y.ID), pos(x.ID))
	}
}
