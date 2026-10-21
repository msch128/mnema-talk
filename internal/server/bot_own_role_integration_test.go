//go:build integration

package server

import (
	"net/http"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/bot"
	"github.com/msch128/mnema-talk/internal/perm"
)

// ownRoleOf returns the managed role of one bot from the role list.
func ownRoleOf(t *testing.T, c *client, botUser uuid.UUID) (perm.Role, bool) {
	t.Helper()
	var list []perm.Role
	c.get("/api/roles").decode(t, &list)
	for _, r := range list {
		if r.BotUserID != nil && *r.BotUserID == botUser {
			return r, true
		}
	}
	return perm.Role{}, false
}

func TestEveryBotHasAManagedRoleOfItsOwn(t *testing.T) {
	a := newApp(t, false)
	owner := a.seedAdmin()
	max := a.register(owner, "max")
	b := createBot(t, owner, "helper")
	other := createBot(t, owner, "other")

	role, ok := ownRoleOf(t, owner, b.UserID)
	if !ok {
		t.Fatalf("own role missing for bot %s", b.UserID)
	}
	if !role.Managed || role.Name != "helper" {
		t.Fatalf("unexpected role metadata: %+v", role)
	}
	if role.Permissions != 0 || role.Position != 1 {
		t.Fatalf("unexpected role permissions or position: %+v", role)
	}
	if role.Hoist || role.Everyone {
		t.Fatalf("unexpected role flags: %+v", role)
	}
	roleID := role.ID.String()
	holds := func(user, role uuid.UUID) bool {
		for _, m := range membersOf(t, owner) {
			if m.ID == user {
				return slices.Contains(m.RoleIDs, role)
			}
		}
		t.Fatal("not in the member list")
		return false
	}
	if !holds(b.UserID, role.ID) || holds(other.UserID, role.ID) {
		t.Fatal("only its bot holds the role")
	}

	// A rename follows the bot.
	wantStatus(t, "rename bot", owner.patch("/api/admin/bots/"+b.ID.String(), map[string]any{"name": "helper2"}), http.StatusOK)
	if r, ok := ownRoleOf(t, owner, b.UserID); !ok || r.Name != "helper2" {
		t.Fatalf("role name after rename: %q (found: %v)", r.Name, ok)
	}
	// Not by hand, not deletable, not assignable to anyone else.
	wantStatus(t, "rename role", owner.patch("/api/admin/roles/"+roleID, map[string]any{"name": "Robots"}), http.StatusBadRequest)
	wantStatus(t, "delete role", owner.delete("/api/admin/roles/"+roleID), http.StatusBadRequest)
	wantStatus(t, "to a human", owner.put("/api/admin/users/"+max.user.ID.String()+"/roles", map[string]any{"role_ids": []uuid.UUID{role.ID}}), http.StatusBadRequest)
	wantStatus(t, "to another bot", owner.put("/api/admin/bots/"+other.ID.String()+"/roles", map[string]any{"role_ids": []uuid.UUID{role.ID}}), http.StatusBadRequest)
	if _, err := a.db.Exec(t.Context(), `INSERT INTO user_roles (user_id, role_id) VALUES ($1, $2)`, other.UserID, role.ID); err == nil {
		t.Fatal("the database let another bot hold it")
	}
	if _, err := a.db.Exec(t.Context(), `INSERT INTO user_roles (user_id, role_id) VALUES ($1, $2)`, max.user.ID, role.ID); err == nil {
		t.Fatal("the database let a human hold it")
	}
	if _, err := a.db.Exec(t.Context(), `DELETE FROM roles WHERE id = $1`, role.ID); err == nil {
		t.Fatal("the database let a live bot's role go")
	}
	// It cannot be taken from the bot, with or without a list.
	wantStatus(t, "empty list", owner.put("/api/admin/bots/"+b.ID.String()+"/roles", map[string]any{"role_ids": []uuid.UUID{}}), http.StatusOK)
	if !holds(b.UserID, role.ID) {
		t.Fatal("the own role was removed")
	}
	// Colour and permissions are editable; the upload limit is not.
	wantStatus(t, "edit", owner.patch("/api/admin/roles/"+roleID, map[string]any{"color": 0x5865F2, "permissions": perm.ViewChannel | perm.SendMessages}), http.StatusOK)
	if r, ok := ownRoleOf(t, owner, b.UserID); !ok || r.Color != 0x5865F2 || r.Permissions != perm.ViewChannel|perm.SendMessages {
		t.Fatalf("after edit: %+v (found: %v)", r, ok)
	}
	wantStatus(t, "upload limit", owner.patch("/api/admin/roles/"+roleID, map[string]any{"max_upload_mb": 5}), http.StatusBadRequest)

	// The hierarchy list ignores own roles, whether listed or not.
	mine := createRole(t, owner, "Mine", perm.ViewChannel)
	wantStatus(t, "order with own role", owner.put("/api/admin/roles/order", map[string]any{"role_ids": []uuid.UUID{mine, role.ID}}), http.StatusNoContent)
	wantStatus(t, "order without", owner.put("/api/admin/roles/order", map[string]any{"role_ids": []uuid.UUID{mine}}), http.StatusNoContent)
	if r, ok := ownRoleOf(t, owner, b.UserID); !ok || r.Position != 1 {
		t.Fatalf("own role moved: %+v (found: %v)", r, ok)
	}

	// Deleting the bot deletes its role, and only its.
	wantStatus(t, "delete bot", owner.delete("/api/admin/bots/"+b.ID.String()), http.StatusNoContent)
	if _, ok := ownRoleOf(t, owner, b.UserID); ok {
		t.Fatal("the role outlived its bot")
	}
	if _, ok := ownRoleOf(t, owner, other.UserID); !ok {
		t.Fatal("another bot's role went too")
	}
}

// A role manager edits the bot's role under the central bot invariant: bits
// they lack, Administrator and admin-area permissions are refused.
func TestOwnRolePermissionsFollowTheBotInvariant(t *testing.T) {
	a := newApp(t, false)
	owner := a.seedAdmin()
	lead := a.register(owner, "lead")
	leadRole := createRole(t, owner, "Role admins", perm.ManageRoles|perm.ManageBots|perm.SendMessages|perm.ViewChannel)
	assignRoles(t, owner, lead, leadRole)
	b := createBot(t, lead, "helper") // an own role without bits is within anyone's rights
	role, ok := ownRoleOf(t, owner, b.UserID)
	if !ok {
		t.Fatal("no own role")
	}
	path := "/api/admin/roles/" + role.ID.String()

	wantStatus(t, "bits lead lacks", lead.patch(path, map[string]any{"permissions": perm.SendMessages | perm.ManageChannels}), http.StatusForbidden)
	wantStatus(t, "elevated", lead.patch(path, map[string]any{"permissions": perm.ManageRoles}), http.StatusForbidden)
	wantStatus(t, "within lead's bits", lead.patch(path, map[string]any{"permissions": perm.SendMessages}), http.StatusOK)
	wantStatus(t, "owner may elevate", owner.patch(path, map[string]any{"permissions": perm.Administrator}), http.StatusOK)
	// Once elevated, only the Owner changes it further.
	wantStatus(t, "lead cannot touch an elevated bot's role", lead.patch(path, map[string]any{"permissions": perm.SendMessages}), http.StatusForbidden)
}

// The migration's backfill gives every bot an empty role of its own and
// changes nobody's effective permissions.
func TestOwnRoleBackfill(t *testing.T) {
	a := newApp(t, false)
	owner := a.seedAdmin()
	b := createBot(t, owner, "legacy")
	granted := createRole(t, owner, "Posters", perm.ViewChannel|perm.SendMessages)
	wantStatus(t, "grant", owner.put("/api/admin/bots/"+b.ID.String()+"/roles", map[string]any{"role_ids": []uuid.UUID{granted}}), http.StatusOK)

	// Take the role away the way a database from before the migration looked.
	if _, err := a.db.Exec(t.Context(), `DELETE FROM user_roles WHERE role_id IN (SELECT id FROM roles WHERE bot_user_id IS NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	// An operator drops the role behind the guard's back.
	if _, err := a.db.Exec(t.Context(), `
		ALTER TABLE roles DISABLE TRIGGER roles_managed_delete_guard;
		DELETE FROM roles WHERE bot_user_id IS NOT NULL;
		ALTER TABLE roles ENABLE TRIGGER roles_managed_delete_guard;`); err != nil {
		t.Fatal(err)
	}
	wantStatus(t, "refresh", owner.patch("/api/admin/bots/"+b.ID.String(), map[string]any{"name": "legacy"}), http.StatusOK)
	if _, ok := ownRoleOf(t, owner, b.UserID); ok {
		t.Fatal("setup: the role is still there")
	}
	// The migration's statements.
	if _, err := a.db.Exec(t.Context(), `
		INSERT INTO roles (name, color, position, permissions, hoist, mentionable, managed, bot_user_id)
		SELECT left(b.name, 32), 0, 1, 0, FALSE, FALSE, TRUE, b.user_id FROM bots b;
		INSERT INTO user_roles (user_id, role_id)
		SELECT r.bot_user_id, r.id FROM roles r WHERE r.bot_user_id IS NOT NULL
		ON CONFLICT DO NOTHING;`); err != nil {
		t.Fatal(err)
	}
	// Any bot edit drops the permission cache the direct changes bypassed.
	wantStatus(t, "refresh again", owner.patch("/api/admin/bots/"+b.ID.String(), map[string]any{"name": "legacy"}), http.StatusOK)
	role, ok := ownRoleOf(t, owner, b.UserID)
	if !ok || role.Name != "legacy" || role.Permissions != 0 {
		t.Fatalf("backfilled role: %+v %v", role, ok)
	}
	var acc bot.Access
	owner.get("/api/admin/bots/"+b.ID.String()+"/access").decode(t, &acc)
	if acc.Permissions != perm.ViewChannel|perm.SendMessages {
		t.Fatalf("effective permissions changed: %b", acc.Permissions)
	}
}
