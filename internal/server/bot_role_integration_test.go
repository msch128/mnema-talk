//go:build integration

package server

import (
	"encoding/json"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/perm"
)

// botRoleOf returns the built-in Bot role from the role list.
func botRoleOf(t *testing.T, c *client) perm.Role {
	t.Helper()
	var list []perm.Role
	c.get("/api/roles").decode(t, &list)
	for _, r := range list {
		if r.Managed && r.BotUserID == nil {
			return r
		}
	}
	t.Fatal("no managed Bot role")
	return perm.Role{}
}

func membersOf(t *testing.T, c *client) []chat.Member {
	t.Helper()
	var list []chat.Member
	res := c.get("/api/members")
	wantStatus(t, "members", res, http.StatusOK)
	res.decode(t, &list)
	return list
}

func TestEveryBotHoldsTheBotRole(t *testing.T) {
	a := newApp(t, false)
	owner := a.seedAdmin()
	max := a.register(owner, "max")
	role := botRoleOf(t, owner)
	if role.Name != "Bot" || role.Permissions != 0 || !role.Hoist || role.Position != 1 {
		t.Fatalf("Bot role: %+v", role)
	}

	b := createBot(t, owner, "helper")
	holds := func() bool {
		for _, m := range membersOf(t, owner) {
			if m.ID == b.UserID {
				return slices.Contains(m.RoleIDs, role.ID)
			}
		}
		t.Fatal("bot is not in the member list")
		return false
	}
	if !holds() {
		t.Fatal("a new bot holds the Bot role")
	}
	// It cannot be taken away: an empty list, or any list, keeps it.
	wantStatus(t, "empty", owner.put("/api/admin/bots/"+b.ID.String()+"/roles", map[string]any{"role_ids": []uuid.UUID{}}), http.StatusOK)
	if !holds() {
		t.Fatal("the Bot role was removed from a bot")
	}
	// Humans cannot hold it: not through either route, not even the Owner.
	wantStatus(t, "assign to a human", owner.put("/api/admin/users/"+max.user.ID.String()+"/roles", map[string]any{"role_ids": []uuid.UUID{role.ID}}), http.StatusBadRequest)
	if _, err := a.db.Exec(t.Context(), `INSERT INTO user_roles (user_id, role_id) VALUES ($1, $2)`, max.user.ID, role.ID); err == nil {
		t.Fatal("the database let a human hold the Bot role")
	}
	// It cannot be deleted, nor renamed; colour, hoist and position are free.
	wantStatus(t, "delete", owner.do(http.MethodDelete, "/api/admin/roles/"+role.ID.String(), nil, nil), http.StatusBadRequest)
	if _, err := a.db.Exec(t.Context(), `DELETE FROM roles WHERE id = $1`, role.ID); err == nil {
		t.Fatal("the database let the Bot role go")
	}
	wantStatus(t, "rename", owner.patch("/api/admin/roles/"+role.ID.String(), map[string]any{"name": "Robots"}), http.StatusBadRequest)
	wantStatus(t, "same name", owner.patch("/api/admin/roles/"+role.ID.String(), map[string]any{"name": "Bot", "color": 0x5865F2, "hoist": false}), http.StatusOK)
	if r := botRoleOf(t, owner); r.Color != 0x5865F2 || r.Hoist || r.Name != "Bot" {
		t.Fatalf("after edit: %+v", r)
	}
	// Reordering lists the Bot role like any other, and a list without it
	// (older clients) keeps working.
	other := createRole(t, owner, "Other", perm.ViewChannel)
	wantStatus(t, "order with", owner.put("/api/admin/roles/order", map[string]any{"role_ids": []uuid.UUID{role.ID, other}}), http.StatusNoContent)
	if r := botRoleOf(t, owner); r.Position != 2 {
		t.Fatalf("moved to the top: %+v", r)
	}
	wantStatus(t, "order without", owner.put("/api/admin/roles/order", map[string]any{"role_ids": []uuid.UUID{other}}), http.StatusNoContent)
	if r := botRoleOf(t, owner); r.Position != 1 {
		t.Fatalf("omitted, it goes to the bottom: %+v", r)
	}
}

func TestBotRoleBackfillAndNewRolesStayAboveIt(t *testing.T) {
	a := newApp(t, false)
	owner := a.seedAdmin()
	b := createBot(t, owner, "helper")
	// The migration's backfill gives a bot without the role (one from before
	// the role existed) the role, and leaves the others alone.
	if _, err := a.db.Exec(t.Context(), `DELETE FROM user_roles WHERE user_id = $1`, b.UserID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(t.Context(), `
		INSERT INTO user_roles (user_id, role_id)
		SELECT b.user_id, r.id FROM bots b CROSS JOIN roles r WHERE r.managed AND r.bot_user_id IS NULL
		ON CONFLICT DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	role := botRoleOf(t, owner)
	var n int
	if err := a.db.QueryRow(t.Context(), `SELECT COUNT(*) FROM user_roles WHERE user_id = $1 AND role_id = $2`, b.UserID, role.ID).Scan(&n); err != nil || n != 1 {
		t.Fatalf("backfill: %d %v", n, err)
	}
	// Roles created later sit above the Bot role, so whoever manages bots
	// outranks it.
	id := createRole(t, owner, "Wardens", perm.ManageBots)
	var list []perm.Role
	owner.get("/api/roles").decode(t, &list)
	pos := map[uuid.UUID]int{}
	for _, r := range list {
		pos[r.ID] = r.Position
	}
	if pos[id] <= pos[role.ID] || pos[role.ID] != 1 {
		t.Fatalf("positions: %v", pos)
	}
}

// The central bot invariant covers edits of the Bot role, which every bot
// holds, even before the first bot exists.
func TestBotRolePermissionsFollowTheBotInvariant(t *testing.T) {
	a := newApp(t, false)
	owner := a.seedAdmin()
	lead := a.register(owner, "lead")
	admin := a.register(owner, "admin")
	leadRole := createRole(t, owner, "Role admins", perm.ManageRoles|perm.ManageBots|perm.SendMessages|perm.ViewChannel)
	adminRole := createRole(t, owner, "Administrators", perm.Administrator)
	assignRoles(t, owner, lead, leadRole)
	assignRoles(t, owner, admin, adminRole)
	role := botRoleOf(t, owner)
	path := "/api/admin/roles/" + role.ID.String()

	// No bot exists yet; the rules apply all the same.
	wantStatus(t, "bits you lack", lead.patch(path, map[string]any{"permissions": perm.SendMessages | perm.ManageChannels}), http.StatusForbidden)
	wantStatus(t, "within your bits", lead.patch(path, map[string]any{"permissions": perm.SendMessages}), http.StatusOK)
	// An Administrator holds every bit but still cannot make bots elevated.
	wantStatus(t, "Administrator for bots", admin.patch(path, map[string]any{"permissions": perm.Administrator}), http.StatusForbidden)
	wantStatus(t, "admin area for bots", admin.patch(path, map[string]any{"permissions": perm.ManageMembers}), http.StatusForbidden)
	wantStatus(t, "plain bits", admin.patch(path, map[string]any{"permissions": perm.SendMessages | perm.AddReactions}), http.StatusOK)

	// A manager cannot create a bot that starts with more than they hold.
	b := createBot(t, lead, "lead-bot") // the Bot role holds what Lead holds
	if b.ID == uuid.Nil {
		t.Fatal("bot not created")
	}
	wantStatus(t, "owner raises", owner.patch(path, map[string]any{"permissions": perm.Administrator}), http.StatusOK)
	// Now the Bot role is Administrator: only the Owner can make bots.
	wantStatus(t, "lead cannot create an elevated bot", lead.post("/api/admin/bots", map[string]any{"name": "second"}), http.StatusForbidden)
	wantStatus(t, "owner can", owner.post("/api/admin/bots", map[string]any{"name": "third"}), http.StatusCreated)
}

func TestMemberListShowsBotsAndNotWebhookAuthors(t *testing.T) {
	a := newApp(t, false)
	owner := a.seedAdmin()
	ch := a.createChannel(owner, "alerts", "text")
	b := createBot(t, owner, "helper")
	makeHook(t, owner, ch, "alerts-hook") // creates the hidden webhook author
	off := createBot(t, owner, "sleeper")
	wantStatus(t, "disable", owner.patch("/api/admin/bots/"+off.ID.String(), map[string]any{"disabled": true}), http.StatusOK)

	var found *chat.Member
	for _, m := range membersOf(t, owner) {
		m := m
		switch {
		case m.ID == b.UserID:
			found = &m
		case m.ID == off.UserID:
			t.Fatal("a disabled bot is listed")
		case m.Username == "Herzog":
		default:
			t.Fatalf("unexpected member %q (webhook author?)", m.Username)
		}
	}
	if found == nil || !found.IsBot || found.DisplayName != "helper" || found.AddedBy == nil || *found.AddedBy != owner.user.ID {
		t.Fatalf("bot entry: %+v", found)
	}
	var hidden int
	if err := a.db.QueryRow(t.Context(), `SELECT COUNT(*) FROM users u WHERE u.is_bot AND NOT EXISTS (SELECT 1 FROM bots b WHERE b.user_id = u.id)`).Scan(&hidden); err != nil || hidden != 1 {
		t.Fatalf("the webhook's hidden account should exist: %d %v", hidden, err)
	}
}

func TestBotsAreMentionableByNameButNotByAllOrHere(t *testing.T) {
	a := newApp(t, false)
	owner := a.seedAdmin()
	ch := a.createChannel(owner, "general", "text")
	b := createBot(t, owner, "helper")
	var username string
	if err := a.db.QueryRow(t.Context(), `SELECT username FROM users WHERE id = $1`, b.UserID).Scan(&username); err != nil {
		t.Fatal(err)
	}
	// Only members who can view the channel are notified: the bot needs View.
	wantStatus(t, "grant view", owner.patch("/api/admin/roles/"+botRoleOf(t, owner).ID.String(), map[string]any{"permissions": perm.ViewChannel}), http.StatusOK)
	mentions := func(content string) []uuid.UUID {
		t.Helper()
		res := owner.post("/api/channels/"+ch.String()+"/messages", map[string]any{"content": content})
		wantStatus(t, "post", res, http.StatusCreated)
		var m struct{ Mentions []uuid.UUID }
		res.decode(t, &m)
		return m.Mentions
	}
	if got := mentions("hi @" + username); !slices.Contains(got, b.UserID) {
		t.Fatalf("mentioning a bot by name: %v", got)
	}
	if got := mentions("@all and @here"); slices.Contains(got, b.UserID) {
		t.Fatalf("@all must not notify bots: %v", got)
	}
}

func TestBotPresenceFollowsTheGatewayConnection(t *testing.T) {
	f := gwFixture(t)
	watcher, _, err := f.owner.dialWS(f.a.origin())
	if err != nil {
		t.Fatal(err)
	}
	snap, ok := watcher.expect("presence_snapshot", soon)
	if !ok {
		t.Fatal("no presence snapshot")
	}
	var before map[uuid.UUID]string
	_ = json.Unmarshal(snap, &before)
	if _, on := before[f.bot.UserID]; on {
		t.Fatalf("a bot without a connection is online: %v", before)
	}

	status := func(want string) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			raw, ok := watcher.expect("presence_update", time.Until(deadline))
			if !ok {
				break
			}
			var p struct {
				UserID uuid.UUID `json:"user_id"`
				Status string    `json:"status"`
			}
			_ = json.Unmarshal(raw, &p)
			if p.UserID == f.bot.UserID && p.Status == want {
				return
			}
		}
		t.Fatalf("no presence update to %q", want)
	}

	g := f.dialGW("")
	g.identify(f.token.Secret, 0)
	status("online")

	// A second connection changes nothing; the last one leaving does.
	g2 := f.dialGW("")
	g2.identify(f.token.Secret, 0)
	g.ws.Close()
	g2.ws.Close()
	status("offline")

	// Someone who connects while the bot is online sees it in the snapshot.
	g3 := f.dialGW("")
	g3.identify(f.token.Secret, 0)
	status("online")
	late, _, err := f.owner.dialWS(f.a.origin())
	if err != nil {
		t.Fatal(err)
	}
	snap, ok = late.expect("presence_snapshot", soon)
	if !ok {
		t.Fatal("no presence snapshot")
	}
	var now map[uuid.UUID]string
	_ = json.Unmarshal(snap, &now)
	if now[f.bot.UserID] != "online" {
		t.Fatalf("snapshot: %v", now)
	}
	g3.ws.Close()
}
