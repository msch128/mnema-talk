//go:build integration

package server

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/perm"
)

func disconnectURL(ch uuid.UUID, user *client) string {
	return "/api/channels/" + ch.String() + "/voice/" + user.user.ID.String() + "/disconnect"
}

// joinTalk seats the member in a Talk and waits until the owner's connection
// has seen it, so the disconnect finds them there.
func joinTalk(t *testing.T, a *app, watcher *wsConn, member *client, ch uuid.UUID) *wsConn {
	t.Helper()
	conn := member.dialLiveWS(a.origin())
	conn.send("voice_join", map[string]any{"channel_id": ch})
	expectVoiceJoin(t, watcher, ch)
	return conn
}

// A server-wide grant disconnects anywhere; a grant through an overwrite on a
// Talk or its category works only there; a deny overwrite withholds it.
func TestDisconnectMembersFollowsTalkOverwrites(t *testing.T) {
	a := newApp(t, true)
	owner := a.seedAdmin()
	global := a.register(owner, "global")
	scoped := a.register(owner, "scoped")
	denied := a.register(owner, "denied")
	max := a.register(owner, "max")

	globalRole := createRole(t, owner, "Mods", perm.DisconnectMembers)
	scopedRole := createRole(t, owner, "Talk helper", 0)
	deniedRole := createRole(t, owner, "Mods but not Quiet", perm.DisconnectMembers)
	assignRoles(t, owner, global, globalRole)
	assignRoles(t, owner, scoped, scopedRole)
	assignRoles(t, owner, denied, deniedRole)

	cat := owner.post("/api/admin/categories", map[string]any{"name": "Area"})
	var category struct{ ID uuid.UUID }
	cat.decode(t, &category)
	res := owner.post("/api/admin/channels", map[string]any{"name": "Inside", "type": "voice", "category_id": category.ID})
	var inside struct{ ID uuid.UUID }
	res.decode(t, &inside)
	lobby := a.createChannel(owner, "Lobby", "voice")
	quiet := a.createChannel(owner, "Quiet", "voice")
	wantStatus(t, "grant on Lobby", putOverwrites(owner, "channels", lobby, overwrite("role", scopedRole, perm.DisconnectMembers, 0)), http.StatusNoContent)
	wantStatus(t, "grant on the category", putOverwrites(owner, "categories", category.ID, overwrite("role", scopedRole, perm.DisconnectMembers, 0)), http.StatusNoContent)
	wantStatus(t, "deny on Quiet", putOverwrites(owner, "channels", quiet, overwrite("role", deniedRole, 0, perm.DisconnectMembers)), http.StatusNoContent)

	watcher := owner.dialLiveWS(a.origin())
	conn := joinTalk(t, a, watcher, max, lobby)

	// Without the permission nothing is removed.
	nobody := a.register(owner, "nobody")
	wantStatus(t, "no permission", nobody.post(disconnectURL(lobby, max), nil), http.StatusForbidden)
	if _, ok := conn.expect("voice_kicked", none); ok {
		t.Fatal("removed without the permission")
	}

	// A grant on the Talk works there only.
	wantStatus(t, "scoped, Quiet", scoped.post(disconnectURL(quiet, max), nil), http.StatusForbidden)
	wantStatus(t, "scoped, Lobby", scoped.post(disconnectURL(lobby, max), nil), http.StatusNoContent)
	if _, ok := conn.expect("voice_kicked", soon); !ok {
		t.Fatal("the disconnect did not arrive")
	}
	// The admin route stays global: the scoped helper has no admin area.
	wantStatus(t, "scoped, admin route", scoped.post("/api/admin/users/"+max.user.ID.String()+"/kick", nil), http.StatusForbidden)

	// The category's grant reaches the Talks inside it.
	conn = joinTalk(t, a, watcher, max, inside.ID)
	wantStatus(t, "scoped, category", scoped.post(disconnectURL(inside.ID, max), nil), http.StatusNoContent)
	if _, ok := conn.expect("voice_kicked", soon); !ok {
		t.Fatal("the category grant did not disconnect")
	}

	// A server-wide grant works in every Talk, except where an overwrite denies it.
	conn = joinTalk(t, a, watcher, max, quiet)
	wantStatus(t, "denied, Quiet", denied.post(disconnectURL(quiet, max), nil), http.StatusForbidden)
	if _, ok := conn.expect("voice_kicked", none); ok {
		t.Fatal("removed despite the deny overwrite")
	}
	wantStatus(t, "global, Quiet", global.post(disconnectURL(quiet, max), nil), http.StatusNoContent)
	if _, ok := conn.expect("voice_kicked", soon); !ok {
		t.Fatal("the server-wide grant did not disconnect")
	}
	annex := a.createChannel(owner, "Annex", "voice")
	conn = joinTalk(t, a, watcher, max, annex)
	wantStatus(t, "denied, elsewhere", denied.post(disconnectURL(annex, max), nil), http.StatusNoContent)
	if _, ok := conn.expect("voice_kicked", soon); !ok {
		t.Fatal("the deny must not reach other Talks")
	}

	// Not in that Talk: 400, like the admin route.
	wantStatus(t, "not in the Talk", global.post(disconnectURL(lobby, max), nil), http.StatusBadRequest)

	// Each disconnect is audited.
	if n := a.countRows(`SELECT COUNT(*) FROM audit_log WHERE action = 'user.talk_kick' AND target_id = $1`, max.user.ID.String()); n != 4 {
		t.Fatalf("audit entries: %d, want 4", n)
	}
}

// The hierarchy and the Owner are protected exactly as on the admin route.
func TestDisconnectMembersKeepsHierarchy(t *testing.T) {
	a := newApp(t, true)
	owner := a.seedAdmin()
	mod := a.register(owner, "mod")
	boss := a.register(owner, "boss")
	peer := a.register(owner, "peer")
	junior := a.register(owner, "junior")
	talk := a.createChannel(owner, "Lobby", "voice")

	bossRole := createRole(t, owner, "Boss", 0)
	modRole := createRole(t, owner, "Mod", 0)
	lowRole := createRole(t, owner, "Low", 0)
	order(t, owner, bossRole, modRole, lowRole)
	assignRoles(t, owner, boss, bossRole)
	assignRoles(t, owner, mod, modRole)
	assignRoles(t, owner, peer, modRole)
	assignRoles(t, owner, junior, lowRole)
	wantStatus(t, "grant", putOverwrites(owner, "channels", talk, overwrite("role", modRole, perm.DisconnectMembers, 0)), http.StatusNoContent)

	watcher := owner.dialLiveWS(a.origin())
	// The Owner answers 400, like on the admin route.
	for target, status := range map[*client]int{boss: http.StatusForbidden, peer: http.StatusForbidden, owner: http.StatusBadRequest} {
		conn := joinTalk(t, a, watcher, target, talk)
		wantStatus(t, "out of reach", mod.post(disconnectURL(talk, target), nil), status)
		if _, ok := conn.expect("voice_kicked", none); ok {
			t.Fatal("removed someone out of reach")
		}
	}
	conn := joinTalk(t, a, watcher, junior, talk)
	wantStatus(t, "junior", mod.post(disconnectURL(talk, junior), nil), http.StatusNoContent)
	if _, ok := conn.expect("voice_kicked", soon); !ok {
		t.Fatal("the disconnect did not arrive")
	}
}
