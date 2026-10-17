//go:build integration

package server

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/perm"
)

func order(t *testing.T, c *client, ids ...uuid.UUID) {
	t.Helper()
	wantStatus(t, "order", c.put("/api/admin/roles/order", map[string]any{"role_ids": ids}), http.StatusNoContent)
}

// A role carries its permissions to whoever gets it: it cannot be handed out
// by someone who lacks those permissions, to others or to themselves.
func TestRolesCannotBeUsedToGainPermissions(t *testing.T) {
	a := newApp(t, false)
	owner := a.seedAdmin()
	mod := a.register(owner, "mod")
	max := a.register(owner, "max")
	webhooks := createRole(t, owner, "Hooks", perm.ManageWebhooks)
	admin := createRole(t, owner, "Boss", perm.Administrator)
	modRole := createRole(t, owner, "Mod", perm.ManageRoles|perm.ManageMembers)
	order(t, owner, modRole, webhooks, admin)
	assignRoles(t, owner, mod, modRole)

	for name, role := range map[string]uuid.UUID{"webhooks": webhooks, "administrator": admin} {
		wantStatus(t, "give "+name+" to another", setRoles(mod, max.user.ID, role), http.StatusForbidden)
		wantStatus(t, "give "+name+" to self", setRoles(mod, mod.user.ID, modRole, role), http.StatusForbidden)
	}
	wantStatus(t, "self change", setRoles(mod, mod.user.ID, modRole), http.StatusForbidden)
	if p := myPerms(t, mod); p.Permissions.Has(perm.ManageWebhooks) || p.Permissions.Has(perm.Administrator) {
		t.Fatalf("the moderator escalated: %b", p.Permissions)
	}
	// Raising a role they may edit beyond what they hold is refused too.
	low := createRole(t, mod, "Low", perm.ViewChannel)
	wantStatus(t, "grow a role", mod.patch("/api/admin/roles/"+low.String(), map[string]any{"permissions": perm.ManageWebhooks}), http.StatusForbidden)
	// The Owner can.
	assignRoles(t, owner, max, webhooks)
}

// Overwrites are bound by hierarchy and by what the editor holds themselves,
// for adding, changing and removing, for allowing and for denying.
func TestOverwritesCannotBeUsedToGainOrDestroy(t *testing.T) {
	a := newApp(t, false)
	owner := a.seedAdmin()
	mod := a.register(owner, "mod")
	peer := a.register(owner, "peer")
	junior := a.register(owner, "junior")
	ch := a.createChannel(owner, "room", "text")
	every := everyoneID(t, owner)

	boss := createRole(t, owner, "Boss", 0)
	modRole := createRole(t, owner, "ChanMod", perm.ManageChannels|perm.ViewChannel)
	lowRole := createRole(t, owner, "Low", 0)
	order(t, owner, boss, modRole, lowRole)
	assignRoles(t, owner, mod, modRole)
	assignRoles(t, owner, peer, modRole)
	assignRoles(t, owner, junior, lowRole)

	// Higher or equal roles, peers and the Owner are out of reach.
	wantStatus(t, "higher role", putOverwrites(mod, "channels", ch, overwrite("role", boss, 0, perm.SendMessages)), http.StatusForbidden)
	wantStatus(t, "own role", putOverwrites(mod, "channels", ch, overwrite("role", modRole, 0, perm.SendMessages)), http.StatusForbidden)
	wantStatus(t, "a peer", putOverwrites(mod, "channels", ch, overwrite("member", peer.user.ID, 0, perm.SendMessages)), http.StatusForbidden)
	// Denying what you do not hold yourself is refused as well as allowing it.
	wantStatus(t, "deny a bit you lack", putOverwrites(mod, "channels", ch, overwrite("role", lowRole, 0, perm.ManageMessages)), http.StatusForbidden)
	wantStatus(t, "allow a bit you lack", putOverwrites(mod, "channels", ch, overwrite("role", lowRole, perm.ManageMessages, 0)), http.StatusForbidden)
	// What they may do:
	wantStatus(t, "lower role", putOverwrites(mod, "channels", ch, overwrite("role", lowRole, 0, perm.SendMessages)), http.StatusNoContent)
	wantStatus(t, "junior", putOverwrites(mod, "channels", ch, overwrite("role", lowRole, 0, perm.SendMessages), overwrite("member", junior.user.ID, perm.SendMessages, 0)), http.StatusNoContent)
	wantStatus(t, "@everyone", putOverwrites(mod, "channels", ch,
		overwrite("role", lowRole, 0, perm.SendMessages), overwrite("member", junior.user.ID, perm.SendMessages, 0), overwrite("role", every, 0, perm.AddReactions)), http.StatusNoContent)

	// An entry for a higher role set by the Owner survives; removing or
	// changing it is refused, and so is a replace that would drop it.
	wantStatus(t, "owner sets a boss entry", putOverwrites(owner, "channels", ch, overwrite("role", boss, 0, perm.AttachFiles), overwrite("role", lowRole, 0, perm.SendMessages)), http.StatusNoContent)
	wantStatus(t, "dropping the boss entry", putOverwrites(mod, "channels", ch, overwrite("role", lowRole, 0, perm.SendMessages)), http.StatusForbidden)
	wantStatus(t, "changing the boss entry", putOverwrites(mod, "channels", ch, overwrite("role", boss, 0, perm.SendMessages), overwrite("role", lowRole, 0, perm.SendMessages)), http.StatusForbidden)
	wantStatus(t, "clearing everything", putOverwrites(mod, "channels", ch), http.StatusForbidden)
	wantStatus(t, "re-sending the entry unchanged", putOverwrites(mod, "channels", ch, overwrite("role", boss, 0, perm.AttachFiles), overwrite("role", lowRole, 0, perm.SendMessages)), http.StatusNoContent)
	var back OverwritesResponse
	owner.get("/api/admin/channels/"+ch.String()+"/overwrites").decode(t, &back)
	if len(back.Overwrites) != 2 {
		t.Fatalf("the higher role's entry must survive: %+v", back)
	}
	// Categories follow the same rules.
	catRes := owner.post("/api/admin/categories", map[string]any{"name": "Area"})
	var cat struct{ ID uuid.UUID }
	catRes.decode(t, &cat)
	wantStatus(t, "category, higher role", putOverwrites(mod, "categories", cat.ID, overwrite("role", boss, 0, perm.SendMessages)), http.StatusForbidden)
	wantStatus(t, "category, lower role", putOverwrites(mod, "categories", cat.ID, overwrite("role", lowRole, 0, perm.SendMessages)), http.StatusNoContent)
}

// Removing someone from a Talk needs the permission in that Talk, and a Talk
// the caller cannot use answers like no Talk at all.
func TestKickIsScopedToTheTalk(t *testing.T) {
	a := newApp(t, true)
	owner := a.seedAdmin()
	mod := a.register(owner, "mod")
	max := a.register(owner, "max")
	open := a.createChannel(owner, "Open", "voice")
	quiet := a.createChannel(owner, "Quiet", "voice")
	hidden := a.createChannel(owner, "Hidden", "voice")
	role := createRole(t, owner, "Talk mod", perm.DisconnectMembers)
	assignRoles(t, owner, mod, role)
	wantStatus(t, "no kicking in Quiet", putOverwrites(owner, "channels", quiet, overwrite("role", role, 0, perm.DisconnectMembers)), http.StatusNoContent)
	wantStatus(t, "hide Hidden", putOverwrites(owner, "channels", hidden, overwrite("member", mod.user.ID, 0, perm.ViewChannel)), http.StatusNoContent)

	watcher, _, err := owner.dialWS(a.origin())
	if err != nil {
		t.Fatal(err)
	}
	conn, _, err := max.dialWS(a.origin())
	if err != nil {
		t.Fatal(err)
	}
	kick := "/api/admin/users/" + max.user.ID.String() + "/kick"
	for _, ch := range []uuid.UUID{quiet, hidden} {
		conn.send("voice_join", map[string]any{"channel_id": ch})
		if _, ok := watcher.expect("voice_state_update", soon); !ok {
			t.Fatal("join not announced")
		}
		res := mod.post(kick, nil)
		if res.status != http.StatusBadRequest {
			t.Fatalf("kick in a Talk the caller may not moderate: %d %s", res.status, res.body)
		}
		if _, ok := conn.expect("voice_kicked", none); ok {
			t.Fatal("the member was removed despite the missing permission")
		}
	}
	conn.send("voice_join", map[string]any{"channel_id": open})
	if _, ok := watcher.expect("voice_state_update", soon); !ok {
		t.Fatal("join not announced")
	}
	wantStatus(t, "kick in Open", mod.post(kick, nil), http.StatusNoContent)
	if _, ok := conn.expect("voice_kicked", soon); !ok {
		t.Fatal("kick did not arrive")
	}
}
