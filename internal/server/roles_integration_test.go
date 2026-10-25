//go:build integration

package server

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/roles"
)

// createRole makes a role through the admin API and returns its ID.
func createRole(t *testing.T, c *client, name string, p perm.Set) uuid.UUID {
	t.Helper()
	res := c.post("/api/admin/roles", map[string]any{"name": name, "permissions": p})
	if res.status != http.StatusCreated {
		t.Fatalf("create role %s: %d %s", name, res.status, res.body)
	}
	var role perm.Role
	res.decode(t, &role)
	return role.ID
}

// assignRoles replaces a member's roles, expecting success.
func assignRoles(t *testing.T, c *client, member *client, ids ...uuid.UUID) {
	t.Helper()
	if res := setRoles(c, member.user.ID, ids...); res.status != http.StatusNoContent {
		t.Fatalf("assign roles: %d %s", res.status, res.body)
	}
}

func setRoles(c *client, user uuid.UUID, ids ...uuid.UUID) response {
	if ids == nil {
		ids = []uuid.UUID{}
	}
	return c.put("/api/admin/users/"+user.String()+"/roles", map[string]any{"role_ids": ids})
}

func overwrite(typ string, id uuid.UUID, allow, deny perm.Set) map[string]any {
	return map[string]any{"type": typ, "id": id, "allow": allow, "deny": deny}
}

func putOverwrites(c *client, kind string, id uuid.UUID, list ...map[string]any) response {
	if list == nil {
		list = []map[string]any{}
	}
	return c.put("/api/admin/"+kind+"/"+id.String()+"/overwrites", map[string]any{"overwrites": list})
}

func everyoneID(t *testing.T, c *client) uuid.UUID {
	t.Helper()
	res := c.get("/api/roles")
	if res.status != http.StatusOK {
		t.Fatalf("roles: %d %s", res.status, res.body)
	}
	var all []perm.Role
	res.decode(t, &all)
	for _, r := range all {
		if r.Everyone {
			return r.ID
		}
	}
	t.Fatal("no @everyone role")
	return uuid.Nil
}

func myPerms(t *testing.T, c *client) roles.PermissionsInfo {
	t.Helper()
	res := c.get("/api/permissions")
	if res.status != http.StatusOK {
		t.Fatalf("permissions: %d %s", res.status, res.body)
	}
	var info roles.PermissionsInfo
	res.decode(t, &info)
	return info
}

// channelPerms is what a permissions answer says about one channel (0 when
// the channel is not listed, i.e. not viewable).
func channelPerms(info roles.PermissionsInfo, ch uuid.UUID) perm.Set {
	for _, c := range info.Channels {
		if c.ChannelID == ch {
			return c.Permissions
		}
	}
	return 0
}

func visibleChannelIDs(t *testing.T, c *client) map[uuid.UUID]bool {
	t.Helper()
	res := c.get("/api/channels")
	if res.status != http.StatusOK {
		t.Fatalf("channels: %d %s", res.status, res.body)
	}
	var h struct {
		Categories []struct {
			ID       uuid.UUID
			Channels []struct{ ID uuid.UUID }
		}
		Uncategorized []struct{ ID uuid.UUID }
	}
	res.decode(t, &h)
	out := map[uuid.UUID]bool{}
	for _, cat := range h.Categories {
		for _, ch := range cat.Channels {
			out[ch.ID] = true
		}
	}
	for _, ch := range h.Uncategorized {
		out[ch.ID] = true
	}
	return out
}

func categoryIDs(t *testing.T, c *client) map[uuid.UUID]bool {
	t.Helper()
	var h struct{ Categories []struct{ ID uuid.UUID } }
	c.get("/api/channels").decode(t, &h)
	out := map[uuid.UUID]bool{}
	for _, cat := range h.Categories {
		out[cat.ID] = true
	}
	return out
}

func wantStatus(t *testing.T, what string, res response, status int) {
	t.Helper()
	if res.status != status {
		t.Fatalf("%s: got %d %s, want %d", what, res.status, res.body, status)
	}
}

// Upgrading must change nothing: a member without any role does exactly what
// members did before roles existed, and nothing more.
func TestDefaultsKeepTodaysBehavior(t *testing.T) {
	a := newApp(t, false)
	owner := a.seedAdmin()
	max := a.register(owner, "max")
	text := a.createChannel(owner, "general", "text")
	voice := a.createChannel(owner, "Lounge", "voice")

	info := myPerms(t, max)
	if info.Owner || info.Permissions != perm.DefaultEveryone || len(info.RoleIDs) != 0 || info.TopPosition != 0 {
		t.Fatalf("a plain member: %+v", info)
	}
	if channelPerms(info, text)&perm.SendMessages == 0 || channelPerms(info, voice)&perm.Connect == 0 || len(info.Catalog) != len(perm.Catalog) {
		t.Fatalf("channel permissions: %+v", info.Channels)
	}
	if !myPerms(t, owner).Owner || myPerms(t, owner).Permissions != perm.All {
		t.Fatal("the Owner holds everything")
	}

	msg := postMessage(t, max, text, map[string]any{"content": "hello @all"})
	wantStatus(t, "react", max.post("/api/messages/"+msg.String()+"/reactions", map[string]string{"emoji": "👍"}), http.StatusOK)
	wantStatus(t, "upload", max.upload("/api/channels/"+text.String()+"/upload", "file", "a.png", "image/png", pngBytes, nil), http.StatusCreated)
	if got := visibleChannelIDs(t, max); !got[text] || !got[voice] {
		t.Fatalf("a member sees every channel: %v", got)
	}
	// Edit own, delete own; never somebody else's.
	wantStatus(t, "edit own", max.put("/api/channels/"+text.String()+"/messages/"+msg.String(), map[string]string{"content": "edited"}), http.StatusOK)
	foreign := postMessage(t, owner, text, map[string]any{"content": "owner says"})
	wantStatus(t, "delete foreign", max.delete("/api/channels/"+text.String()+"/messages/"+foreign.String()), http.StatusForbidden)
	wantStatus(t, "delete own", max.delete("/api/channels/"+text.String()+"/messages/"+msg.String()), http.StatusNoContent)
	wantStatus(t, "owner deletes foreign", owner.delete("/api/channels/"+text.String()+"/messages/"+foreign.String()), http.StatusNoContent)

	// The admin area stays closed to members.
	for _, c := range []struct{ method, path string }{
		{"POST", "/api/admin/channels"}, {"POST", "/api/admin/categories"}, {"GET", "/api/admin/invites"},
		{"POST", "/api/admin/invites"}, {"GET", "/api/admin/users"}, {"POST", "/api/admin/roles"},
		{"GET", "/api/admin/system"}, {"GET", "/api/admin/media"}, {"PUT", "/api/admin/layout"},
	} {
		if res := max.do(c.method, c.path, map[string]any{}, nil); res.status != http.StatusForbidden {
			t.Fatalf("%s %s as a member: %d %s", c.method, c.path, res.status, res.body)
		}
	}
	wantStatus(t, "roles are visible to members", max.get("/api/roles"), http.StatusOK)
}

// Nobody can edit, restrict, demote, kick, disable, rename or overwrite the
// Owner, whatever they hold, Administrator included.
func TestTheOwnerCannotBeTouched(t *testing.T) {
	a := newApp(t, false)
	owner := a.seedAdmin()
	boss := a.register(owner, "boss")
	text := a.createChannel(owner, "general", "text")
	all := createRole(t, owner, "Everything", perm.All)
	assignRoles(t, owner, boss, all)

	if p := myPerms(t, boss); p.Owner || p.Permissions != perm.All {
		t.Fatalf("an Administrator holds every permission: %+v", p)
	}
	id := owner.user.ID.String()
	for _, c := range []struct {
		what, method, path string
		body               any
		status             int
	}{
		{"set roles", "PUT", "/api/admin/users/" + id + "/roles", map[string]any{"role_ids": []uuid.UUID{all}}, http.StatusForbidden},
		{"disable", "POST", "/api/admin/users/" + id + "/disable", nil, http.StatusBadRequest},
		{"enable", "POST", "/api/admin/users/" + id + "/enable", nil, http.StatusBadRequest},
		{"revoke sessions", "POST", "/api/admin/users/" + id + "/sessions/revoke", nil, http.StatusBadRequest},
		{"kick from Talk", "POST", "/api/admin/users/" + id + "/kick", nil, http.StatusBadRequest},
		{"edit status", "PUT", "/api/admin/users/" + id + "/status", map[string]string{"status_text": "hacked"}, http.StatusBadRequest},
		{"reset password", "POST", "/api/admin/users/" + id + "/password-reset", nil, http.StatusForbidden},
		{"set password", "POST", "/api/admin/users/" + id + "/password", map[string]string{"password": "a-new-password-123"}, http.StatusForbidden},
		{"overwrite for the Owner", "PUT", "/api/admin/channels/" + text.String() + "/overwrites",
			map[string]any{"overwrites": []map[string]any{overwrite("member", owner.user.ID, 0, perm.ViewChannel)}}, http.StatusForbidden},
	} {
		var res response
		if c.method == "PUT" {
			res = boss.put(c.path, c.body)
		} else {
			res = boss.post(c.path, c.body)
		}
		if res.status != c.status {
			t.Errorf("%s: got %d %s, want %d", c.what, res.status, res.body, c.status)
		}
	}
	// Owner-only areas stay closed to every role.
	for _, path := range []string{"/api/admin/system", "/api/admin/media/stats", "/api/admin/system/update"} {
		wantStatus(t, "boss "+path, boss.get(path), http.StatusForbidden)
	}
	// The Owner keeps every permission whatever is configured.
	wantStatus(t, "deny everything to @everyone",
		owner.patch("/api/admin/roles/"+everyoneID(t, owner).String(), map[string]any{"permissions": 0}), http.StatusOK)
	wantStatus(t, "owner posts", owner.post("/api/channels/"+text.String()+"/messages", map[string]any{"content": "still here"}), http.StatusCreated)
	if p := myPerms(t, owner); p.Permissions != perm.All || !channelPerms(p, text).Has(perm.SendMessages) {
		t.Fatalf("owner after lockdown: %+v", p)
	}
	// The Owner stays visible in the member list and cannot get a role.
	var members []struct {
		ID      uuid.UUID
		Role    string
		RoleIDs []uuid.UUID `json:"role_ids"`
	}
	boss.get("/api/members").decode(t, &members)
	for _, m := range members {
		if m.ID == owner.user.ID && (m.Role != "admin" || len(m.RoleIDs) != 0) {
			t.Fatalf("owner entry: %+v", m)
		}
		if m.ID == boss.user.ID && (len(m.RoleIDs) != 1 || m.RoleIDs[0] != all) {
			t.Fatalf("boss entry: %+v", m)
		}
	}
}

func TestRoleManagementAndHierarchy(t *testing.T) {
	a := newApp(t, false)
	owner := a.seedAdmin()
	mod := a.register(owner, "mod")
	helper := a.register(owner, "helper")
	max := a.register(owner, "max")

	// mod holds Manage roles and Manage members; the Owner built the roles.
	top := createRole(t, owner, "Moderator", perm.ManageRoles|perm.ManageMembers|perm.CreateInvites|perm.ViewChannel)
	vip := createRole(t, owner, "VIP", perm.ViewChannel) // created later: sits below Moderator
	// Order: Moderator on top, VIP below.
	wantStatus(t, "order", owner.put("/api/admin/roles/order", map[string]any{"role_ids": []uuid.UUID{top, vip}}), http.StatusNoContent)
	assignRoles(t, owner, mod, top)

	var list []perm.Role
	mod.get("/api/roles").decode(t, &list)
	if len(list) != 4 || list[0].ID != top || list[1].ID != vip || !list[2].Managed || !list[3].Everyone {
		t.Fatalf("roles: %+v", list)
	}
	if p := myPerms(t, mod); p.TopPosition != 3 || len(p.RoleIDs) != 1 {
		t.Fatalf("mod position: %+v", p)
	}

	// mod can create roles (at the bottom) but not above themselves, and not
	// with permissions they lack.
	res := mod.post("/api/admin/roles", map[string]any{"name": "Newcomer", "permissions": perm.ViewChannel | perm.SendMessages})
	wantStatus(t, "create below", res, http.StatusCreated)
	var newcomer perm.Role
	res.decode(t, &newcomer)
	if newcomer.Position != 2 { // just above the Bot role at the bottom
		t.Fatalf("new roles start at the bottom: %+v", newcomer)
	}
	wantStatus(t, "grant what you lack", mod.post("/api/admin/roles", map[string]any{"name": "Sneaky", "permissions": perm.ManageChannels}), http.StatusForbidden)
	wantStatus(t, "grant administrator", mod.post("/api/admin/roles", map[string]any{"name": "Sneaky", "permissions": perm.Administrator}), http.StatusForbidden)
	wantStatus(t, "unknown bits", owner.post("/api/admin/roles", map[string]any{"name": "Odd", "permissions": 1 << 40}), http.StatusBadRequest)
	wantStatus(t, "empty name", owner.post("/api/admin/roles", map[string]any{"name": "  "}), http.StatusBadRequest)
	wantStatus(t, "reserved name", owner.post("/api/admin/roles", map[string]any{"name": "@everyone"}), http.StatusBadRequest)
	wantStatus(t, "bad color", owner.post("/api/admin/roles", map[string]any{"name": "Pink", "color": 1 << 30}), http.StatusBadRequest)

	// mod edits roles below, not their own or higher ones.
	wantStatus(t, "edit below", mod.patch("/api/admin/roles/"+newcomer.ID.String(), map[string]any{"name": "Rookie", "color": 0xff0000, "hoist": true}), http.StatusOK)
	wantStatus(t, "edit own top role", mod.patch("/api/admin/roles/"+top.String(), map[string]any{"name": "Mine"}), http.StatusForbidden)
	wantStatus(t, "bits you lack", mod.patch("/api/admin/roles/"+newcomer.ID.String(), map[string]any{"permissions": perm.ViewChannel | perm.ManageChannels}), http.StatusForbidden)
	wantStatus(t, "delete own top role", mod.delete("/api/admin/roles/"+top.String()), http.StatusForbidden)

	// Assignments: roles below only, members below only.
	assignRoles(t, mod, max, newcomer.ID)
	wantStatus(t, "assign own top role", setRoles(mod, max.user.ID, top), http.StatusForbidden)
	assignRoles(t, owner, helper, top) // a peer of mod
	wantStatus(t, "touch a peer", setRoles(mod, helper.user.ID, newcomer.ID), http.StatusForbidden)
	wantStatus(t, "unknown role", setRoles(mod, max.user.ID, uuid.New()), http.StatusBadRequest)
	wantStatus(t, "unknown member", setRoles(mod, uuid.New()), http.StatusNotFound)
	wantStatus(t, "@everyone is implicit", setRoles(mod, max.user.ID, everyoneID(t, mod)), http.StatusBadRequest)

	// Reordering: mod can only shuffle what is below their own role.
	other := createRole(t, owner, "Other", 0)
	wantStatus(t, "reorder below", mod.put("/api/admin/roles/order", map[string]any{"role_ids": []uuid.UUID{top, vip, other, newcomer.ID}}), http.StatusNoContent)
	wantStatus(t, "move above self", mod.put("/api/admin/roles/order", map[string]any{"role_ids": []uuid.UUID{vip, top, other, newcomer.ID}}), http.StatusForbidden)
	wantStatus(t, "move self down", mod.put("/api/admin/roles/order", map[string]any{"role_ids": []uuid.UUID{vip, other, top, newcomer.ID}}), http.StatusForbidden)
	wantStatus(t, "incomplete order", mod.put("/api/admin/roles/order", map[string]any{"role_ids": []uuid.UUID{top}}), http.StatusBadRequest)
	wantStatus(t, "duplicate in order", mod.put("/api/admin/roles/order", map[string]any{"role_ids": []uuid.UUID{top, top, vip, other}}), http.StatusBadRequest)

	// Members moderate those below them, never peers.
	wantStatus(t, "disable below", mod.post("/api/admin/users/"+max.user.ID.String()+"/disable", nil), http.StatusNoContent)
	wantStatus(t, "enable below", mod.post("/api/admin/users/"+max.user.ID.String()+"/enable", nil), http.StatusNoContent)
	max = a.login("max", "member-password-123") // disabling ended the session
	wantStatus(t, "disable a peer", mod.post("/api/admin/users/"+helper.user.ID.String()+"/disable", nil), http.StatusForbidden)
	wantStatus(t, "revoke a peer", mod.post("/api/admin/users/"+helper.user.ID.String()+"/sessions/revoke", nil), http.StatusForbidden)
	wantStatus(t, "status of a peer", mod.put("/api/admin/users/"+helper.user.ID.String()+"/status", map[string]string{"status_text": "x"}), http.StatusForbidden)
	wantStatus(t, "status below", mod.put("/api/admin/users/"+max.user.ID.String()+"/status", map[string]string{"status_text": "hi"}), http.StatusOK)
	wantStatus(t, "no password reset for roles", mod.post("/api/admin/users/"+max.user.ID.String()+"/password-reset", nil), http.StatusForbidden)
	// The kick needs its own permission.
	wantStatus(t, "kick without the permission", mod.post("/api/admin/users/"+max.user.ID.String()+"/kick", nil), http.StatusForbidden)

	// The account list shows role_ids to those who manage members or roles.
	var users []struct {
		ID      uuid.UUID
		RoleIDs []uuid.UUID `json:"role_ids"`
	}
	mod.get("/api/admin/users").decode(t, &users)
	found := false
	for _, u := range users {
		if u.ID == max.user.ID {
			found = len(u.RoleIDs) == 1 && u.RoleIDs[0] == newcomer.ID
		}
	}
	if !found {
		t.Fatalf("role_ids missing from the account list: %+v", users)
	}

	// Deleting a role takes it from its holders and closes the gap.
	wantStatus(t, "delete below", mod.delete("/api/admin/roles/"+newcomer.ID.String()), http.StatusNoContent)
	if p := myPerms(t, max); len(p.RoleIDs) != 0 {
		t.Fatalf("deleted role lingers: %+v", p)
	}
	wantStatus(t, "delete unknown", mod.delete("/api/admin/roles/"+uuid.New().String()), http.StatusNotFound)
	wantStatus(t, "delete @everyone", owner.delete("/api/admin/roles/"+everyoneID(t, owner).String()), http.StatusBadRequest)
	wantStatus(t, "edit unknown", owner.patch("/api/admin/roles/"+uuid.New().String(), map[string]any{"name": "x"}), http.StatusNotFound)
	wantStatus(t, "everyone is only about permissions",
		owner.patch("/api/admin/roles/"+everyoneID(t, owner).String(), map[string]any{"name": "all"}), http.StatusBadRequest)

	// Invites follow Create invites.
	wantStatus(t, "mod invites", mod.post("/api/admin/invites", map[string]any{}), http.StatusCreated)
	wantStatus(t, "member invites", max.post("/api/admin/invites", map[string]any{}), http.StatusForbidden)
}

func TestKickNeedsDisconnectMembersAndRank(t *testing.T) {
	a := newApp(t, true)
	owner := a.seedAdmin()
	mod := a.register(owner, "mod")
	max := a.register(owner, "max")
	voice := a.createChannel(owner, "Lounge", "voice")
	role := createRole(t, owner, "Talk mod", perm.ViewChannel|perm.DisconnectMembers)
	assignRoles(t, owner, mod, role)

	watcher := owner.dialLiveWS(a.origin())
	conn, _, err := max.dialWS(a.origin())
	if err != nil {
		t.Fatal(err)
	}
	conn.send("voice_join", map[string]any{"channel_id": voice})
	if _, ok := watcher.expect("voice_state_update", 2*time.Second); !ok {
		t.Fatal("the join was not announced")
	}
	wantStatus(t, "kick", mod.post("/api/admin/users/"+max.user.ID.String()+"/kick", nil), http.StatusNoContent)
	if _, ok := conn.expect("voice_kicked", 2*time.Second); !ok {
		t.Fatal("the member was not told")
	}
	wantStatus(t, "max cannot kick", max.post("/api/admin/users/"+mod.user.ID.String()+"/kick", nil), http.StatusForbidden)
	// A peer (not below) cannot be kicked: give max the same role.
	assignRoles(t, owner, max, role)
	wantStatus(t, "kick a peer", mod.post("/api/admin/users/"+max.user.ID.String()+"/kick", nil), http.StatusForbidden)
}
