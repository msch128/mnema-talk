//go:build integration

package server

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/bot"
	"github.com/msch128/mnema-talk/internal/perm"
)

func createBot(t *testing.T, c *client, name string) bot.Bot {
	t.Helper()
	res := c.post("/api/admin/bots", map[string]any{"name": name})
	if res.status != http.StatusCreated {
		t.Fatalf("create bot: %d %s", res.status, res.body)
	}
	var b bot.Bot
	res.decode(t, &b)
	return b
}

func issueToken(t *testing.T, c *client, id uuid.UUID, body map[string]any) bot.NewTokenResponse {
	t.Helper()
	res := c.post("/api/admin/bots/"+id.String()+"/tokens", body)
	if res.status != http.StatusCreated {
		t.Fatalf("issue token: %d %s", res.status, res.body)
	}
	var tok bot.NewTokenResponse
	res.decode(t, &tok)
	return tok
}

func TestBotManagementNeedsManageBots(t *testing.T) {
	a := newApp(t, false)
	owner := a.seedAdmin()
	max := a.register(owner, "max")
	wantStatus(t, "plain member", max.post("/api/admin/bots", map[string]any{"name": "x"}), http.StatusForbidden)
	wantStatus(t, "plain member list", max.get("/api/admin/bots"), http.StatusForbidden)

	// Manage roles alone does not open bots; and nobody but the Owner grants Manage bots.
	maker := createRole(t, owner, "Bot makers", perm.ManageBots)
	assignRoles(t, owner, max, maker)
	b := createBot(t, max, "helper")
	if b.Name != "helper" || b.AllowedIntents != 0 || len(b.RoleIDs) != 0 {
		t.Fatalf("a new bot starts empty: %+v", b)
	}
	wantStatus(t, "list", max.get("/api/admin/bots"), http.StatusOK)

	lead := a.register(owner, "lead")
	roleAdmins := createRole(t, owner, "Role admins", perm.ManageRoles|perm.ManageBots)
	assignRoles(t, owner, lead, roleAdmins)
	res := lead.post("/api/admin/roles", map[string]any{"name": "sneaky", "permissions": perm.ManageBots})
	wantStatus(t, "grant manage bots", res, http.StatusForbidden)
	wantStatus(t, "name too long", owner.post("/api/admin/bots", map[string]any{"name": strings.Repeat("x", 25)}), http.StatusBadRequest)
	wantStatus(t, "unknown intents", owner.post("/api/admin/bots", map[string]any{"name": "y", "allowed_intents": 1 << 30}), http.StatusBadRequest)
}

func TestBotRolesAndHierarchy(t *testing.T) {
	a := newApp(t, false)
	owner := a.seedAdmin()
	max := a.register(owner, "max")
	// New roles sit below the older ones, so create the high ones first.
	admins := createRole(t, owner, "Admins", perm.Administrator)
	poster := createRole(t, owner, "Poster", perm.SendMessages)
	maker := createRole(t, owner, "Bot makers", perm.ManageBots|perm.SendMessages)
	assignRoles(t, owner, max, maker)
	b := createBot(t, max, "helper")

	path := "/api/admin/bots/" + b.ID.String()
	// A bot never gets administrator power from anybody but the Owner.
	wantStatus(t, "admin role by non-owner", max.put(path+"/roles", map[string]any{"role_ids": []uuid.UUID{admins}}), http.StatusForbidden)
	wantStatus(t, "role above caller", max.put(path+"/roles", map[string]any{"role_ids": []uuid.UUID{poster}}), http.StatusForbidden)
	wantStatus(t, "owner gives poster", owner.put(path+"/roles", map[string]any{"role_ids": []uuid.UUID{poster}}), http.StatusOK)

	// The generic member route does not touch bots, and the bot route does not touch members.
	wantStatus(t, "members route", setRoles(owner, b.UserID, poster), http.StatusForbidden)
	wantStatus(t, "bot route on a member", owner.put("/api/admin/bots/"+max.user.ID.String()+"/roles", map[string]any{"role_ids": []uuid.UUID{}}), http.StatusNotFound)

	// Poster is above max's role, so max may not manage a bot that holds it.
	wantStatus(t, "bot outranks caller", max.patch(path, map[string]any{"name": "renamed"}), http.StatusForbidden)
	wantStatus(t, "owner renames", owner.patch(path, map[string]any{"name": "renamed"}), http.StatusOK)

	// Owner may give an elevated role; the access view flags it.
	wantStatus(t, "owner gives admin", owner.put(path+"/roles", map[string]any{"role_ids": []uuid.UUID{admins}}), http.StatusOK)
	res := owner.get(path + "/access")
	var acc bot.Access
	res.decode(t, &acc)
	if !acc.Elevated || !acc.Permissions.Has(perm.Administrator) {
		t.Fatalf("access: %s", res.body)
	}
}

func TestBotAccessShowsOnlyGrantedChannels(t *testing.T) {
	a := newApp(t, false)
	owner := a.seedAdmin()
	open := a.createChannel(owner, "open", "text")
	other := a.createChannel(owner, "other", "text")
	b := createBot(t, owner, "helper")
	path := "/api/admin/bots/" + b.ID.String()

	var acc bot.Access
	owner.get(path+"/access").decode(t, &acc)
	if len(acc.Channels) != 0 || acc.Permissions != 0 {
		t.Fatalf("a new bot reaches nothing, not even what @everyone sees: %+v", acc)
	}

	role := createRole(t, owner, "Reader", perm.SendMessages|perm.AddReactions)
	wantStatus(t, "give role", owner.put(path+"/roles", map[string]any{"role_ids": []uuid.UUID{role}}), http.StatusOK)
	wantStatus(t, "allow in one channel", putOverwrites(owner, "channels", open, overwrite("role", role, perm.ViewChannel, 0)), http.StatusNoContent)
	res := owner.get(path + "/access")
	res.decode(t, &acc)
	if len(acc.Channels) != 1 || acc.Channels[0].ChannelID != open || !acc.Channels[0].Permissions.Has(perm.ViewChannel|perm.SendMessages) {
		t.Fatalf("access: %s", res.body)
	}
	_ = other
}

func TestBotTokens(t *testing.T) {
	a := newApp(t, false)
	owner := a.seedAdmin()
	b := createBot(t, owner, "helper")
	path := "/api/admin/bots/" + b.ID.String()

	tok := issueToken(t, owner, b.ID, map[string]any{"name": "ci"})
	if !strings.HasPrefix(tok.Secret, bot.TokenPrefix) || tok.Name != "ci" || tok.State != "active" || tok.Hint != tok.Secret[len(tok.Secret)-4:] {
		t.Fatalf("token: %+v", tok)
	}
	list := owner.get(path + "/tokens")
	if strings.Contains(string(list.body), tok.Secret) {
		t.Fatal("the secret must never be listed")
	}
	var rows []bot.Token
	list.decode(t, &rows)
	if len(rows) != 1 || rows[0].ID != tok.ID {
		t.Fatalf("list: %s", list.body)
	}

	var stored []byte
	if err := a.db.QueryRow(t.Context(), `SELECT token_hash FROM bot_tokens WHERE id = $1`, tok.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if string(stored) == tok.Secret || len(stored) != 32 {
		t.Fatal("only a hash is stored")
	}

	past := time.Now().Add(-time.Hour).Format(time.RFC3339)
	wantStatus(t, "expiry in the past", owner.post(path+"/tokens", map[string]any{"expires_at": past}), http.StatusBadRequest)
	later := time.Now().Add(48 * time.Hour).Format(time.RFC3339)
	exp := issueToken(t, owner, b.ID, map[string]any{"expires_at": later})
	if exp.ExpiresAt == nil {
		t.Fatal("expiry lost")
	}

	// Regenerate keeps name and expiry and ends the old token.
	res := owner.post(path+"/tokens/"+tok.ID.String()+"/regenerate", nil)
	wantStatus(t, "regenerate", res, http.StatusCreated)
	var fresh bot.NewTokenResponse
	res.decode(t, &fresh)
	if fresh.ID == tok.ID || fresh.Secret == tok.Secret || fresh.Name != "ci" {
		t.Fatalf("regenerated: %+v", fresh)
	}
	owner.get(path+"/tokens").decode(t, &rows)
	states := map[uuid.UUID]string{}
	for _, r := range rows {
		states[r.ID] = r.State
	}
	if states[tok.ID] != "revoked" || states[fresh.ID] != "active" {
		t.Fatalf("states: %v", states)
	}
	wantStatus(t, "revoke", owner.delete(path+"/tokens/"+fresh.ID.String()), http.StatusNoContent)
	wantStatus(t, "revoke again", owner.delete(path+"/tokens/"+fresh.ID.String()), http.StatusNoContent)
	wantStatus(t, "unknown token", owner.delete(path+"/tokens/"+uuid.NewString()), http.StatusNotFound)
	wantStatus(t, "unknown bot", owner.get("/api/admin/bots/"+uuid.NewString()+"/tokens"), http.StatusNotFound)

	for i := 0; i < bot.MaxTokens; i++ {
		res := owner.post(path+"/tokens", map[string]any{})
		if i < bot.MaxTokens-1 {
			wantStatus(t, "token", res, http.StatusCreated)
		}
	}
	wantStatus(t, "too many", owner.post(path+"/tokens", map[string]any{}), http.StatusConflict)
}

func TestBotCannotSignIn(t *testing.T) {
	a := newApp(t, false)
	owner := a.seedAdmin()
	b := createBot(t, owner, "helper")
	var username string
	if err := a.db.QueryRow(t.Context(), `SELECT username FROM users WHERE id = $1`, b.UserID).Scan(&username); err != nil {
		t.Fatal(err)
	}
	for _, pw := range []string{"!", "", "password-123456"} {
		res := a.anon().post("/api/auth/login", map[string]string{"username": username, "password": pw})
		if res.status == http.StatusOK {
			t.Fatalf("a bot signed in with %q", pw)
		}
	}
}

func TestBotUpdateDeleteAndLimits(t *testing.T) {
	a := newApp(t, false)
	owner := a.seedAdmin()
	b := createBot(t, owner, "helper")
	path := "/api/admin/bots/" + b.ID.String()

	wantStatus(t, "get", owner.get(path), http.StatusOK)
	wantStatus(t, "bad id", owner.get("/api/admin/bots/nope"), http.StatusBadRequest)
	wantStatus(t, "intents", owner.patch(path, map[string]any{"allowed_intents": bot.IntentGuildMessages | bot.IntentGuilds}), http.StatusOK)
	wantStatus(t, "bad intents", owner.patch(path, map[string]any{"allowed_intents": 4}), http.StatusBadRequest)
	wantStatus(t, "bad name", owner.patch(path, map[string]any{"name": "  "}), http.StatusBadRequest)
	wantStatus(t, "disable", owner.patch(path, map[string]any{"disabled": true}), http.StatusOK)
	var cur bot.Bot
	owner.get(path).decode(t, &cur)
	if !cur.Disabled || cur.AllowedIntents != bot.IntentGuildMessages|bot.IntentGuilds {
		t.Fatalf("bot: %+v", cur)
	}
	wantStatus(t, "unknown", owner.patch("/api/admin/bots/"+uuid.NewString(), map[string]any{"name": "x"}), http.StatusNotFound)

	// A bot with history keeps its account; one without loses it.
	wantStatus(t, "delete", owner.delete(path), http.StatusNoContent)
	wantStatus(t, "gone", owner.get(path), http.StatusNotFound)
	var n int
	if err := a.db.QueryRow(t.Context(), `SELECT COUNT(*) FROM users WHERE id = $1`, b.UserID).Scan(&n); err != nil || n != 0 {
		t.Fatalf("account left: %d %v", n, err)
	}
	if res := owner.get("/api/admin/bots"); res.status != http.StatusOK || strings.TrimSpace(string(res.body)) != "[]" {
		t.Fatalf("list: %s", res.body)
	}
}

// Manage bots is not a way to act with rights one does not have: the bot's
// permissions must be within the caller's for tokens, roles and edits.
func TestBotManagementCannotBorrowTheBotsRights(t *testing.T) {
	a := newApp(t, false)
	owner := a.seedAdmin()
	ch := a.createChannel(owner, "ops", "text")
	max := a.register(owner, "max")
	// Roles created later sit lower: the bot's role must be below max's.
	maker := createRole(t, owner, "Bot makers", perm.ManageBots|perm.ManageChannels|perm.SendMessages)
	botRole := createRole(t, owner, "Moderating bot", perm.SendMessages|perm.ManageMessages)
	assignRoles(t, owner, max, maker)
	b := createBot(t, owner, "janitor")
	path := "/api/admin/bots/" + b.ID.String()
	wantStatus(t, "bot role", owner.put(path+"/roles", map[string]any{"role_ids": []uuid.UUID{botRole}}), http.StatusOK)
	wantStatus(t, "allow", putOverwrites(owner, "channels", ch, overwrite("role", botRole, perm.ViewChannel, 0)), http.StatusNoContent)

	// The bot may delete others' messages; max may not, so max cannot get its token.
	for _, c := range []struct{ what, method, url string }{
		{"issue", "POST", path + "/tokens"}, {"list", "GET", path + "/tokens"},
		{"rename", "PATCH", path}, {"roles", "PUT", path + "/roles"},
	} {
		res := max.do(c.method, c.url, map[string]any{"name": "x", "role_ids": []uuid.UUID{}}, nil)
		wantStatus(t, c.what, res, http.StatusForbidden)
	}
	wantStatus(t, "owner issues", owner.post(path+"/tokens", map[string]any{}), http.StatusCreated)
	// Reading the bot and what it reaches stays possible.
	wantStatus(t, "read", max.get(path), http.StatusOK)
	wantStatus(t, "access", max.get(path+"/access"), http.StatusOK)

	// A bot with a role max fully covers is manageable by max.
	harmless := createRole(t, owner, "Harmless bot", perm.SendMessages)
	h := createBot(t, owner, "echo")
	wantStatus(t, "harmless role", owner.put("/api/admin/bots/"+h.ID.String()+"/roles", map[string]any{"role_ids": []uuid.UUID{harmless}}), http.StatusOK)
	wantStatus(t, "max issues", max.post("/api/admin/bots/"+h.ID.String()+"/tokens", map[string]any{}), http.StatusCreated)
	// ...but max cannot hand that bot more than max holds, directly or through an overwrite.
	wantStatus(t, "overwrite beyond own rights", putOverwrites(max, "channels", ch,
		overwrite("member", h.UserID, perm.ViewChannel|perm.ManageMessages, 0)), http.StatusForbidden)
}

// A role a bot holds must not become a way to give it administrator power.
func TestRoleHeldByBotCannotGainAdminPowerFromAnAdministrator(t *testing.T) {
	a := newApp(t, false)
	owner := a.seedAdmin()
	lead := a.register(owner, "lead")
	admins := createRole(t, owner, "Admins", perm.Administrator)
	botRole := createRole(t, owner, "Plain bot", perm.SendMessages)
	free := createRole(t, owner, "Unused", perm.SendMessages)
	assignRoles(t, owner, lead, admins)
	b := createBot(t, owner, "echo")
	wantStatus(t, "bot role", owner.put("/api/admin/bots/"+b.ID.String()+"/roles", map[string]any{"role_ids": []uuid.UUID{botRole}}), http.StatusOK)

	patch := func(id uuid.UUID, c *client, p perm.Set) response {
		return c.patch("/api/admin/roles/"+id.String(), map[string]any{"permissions": p})
	}
	wantStatus(t, "admin power onto a bot's role", patch(botRole, lead, perm.SendMessages|perm.Administrator), http.StatusForbidden)
	wantStatus(t, "admin-area bit onto a bot's role", patch(botRole, lead, perm.SendMessages|perm.ManageChannels), http.StatusForbidden)
	wantStatus(t, "ordinary bit onto a bot's role", patch(botRole, lead, perm.SendMessages|perm.AddReactions), http.StatusOK)
	wantStatus(t, "same on a role no bot holds", patch(free, lead, perm.SendMessages|perm.ManageChannels), http.StatusOK)
	wantStatus(t, "Owner may", patch(botRole, owner, perm.SendMessages|perm.Administrator), http.StatusOK)

	// An elevated bot is the Owner's: an Administrator cannot mint tokens for it.
	wantStatus(t, "elevated bot token", lead.post("/api/admin/bots/"+b.ID.String()+"/tokens", map[string]any{}), http.StatusForbidden)
	wantStatus(t, "owner token", owner.post("/api/admin/bots/"+b.ID.String()+"/tokens", map[string]any{}), http.StatusCreated)
}

// Disconnect members is a channel permission that opens part of the admin
// area: every way a bot can gain it, through a role, a channel or category
// overwrite or a member overwrite, is the Owner's.
func TestElevatedChannelPermissionsForBotsAreOwnerOnly(t *testing.T) {
	a := newApp(t, false)
	owner := a.seedAdmin()
	ch := a.createChannel(owner, "ops", "text")
	cat := func() uuid.UUID {
		res := owner.post("/api/admin/categories", map[string]any{"name": "Cat"})
		if res.status != http.StatusCreated {
			t.Fatalf("category: %d %s", res.status, res.body)
		}
		var c struct{ ID uuid.UUID }
		res.decode(t, &c)
		return c.ID
	}()
	lead := a.register(owner, "lead")
	admins := createRole(t, owner, "Admins", perm.Administrator)
	assignRoles(t, owner, lead, admins)
	botRole := createRole(t, owner, "Plain bot", perm.SendMessages)
	loose := createRole(t, owner, "Loose", perm.SendMessages)
	b := createBot(t, owner, "echo")
	path := "/api/admin/bots/" + b.ID.String()
	wantStatus(t, "bot role", owner.put(path+"/roles", map[string]any{"role_ids": []uuid.UUID{botRole}}), http.StatusOK)

	disconnect := perm.ViewChannel | perm.DisconnectMembers
	// Role held by a bot, member overwrite of a bot, channel and category alike.
	wantStatus(t, "role overwrite", putOverwrites(lead, "channels", ch, overwrite("role", botRole, disconnect, 0)), http.StatusForbidden)
	wantStatus(t, "member overwrite", putOverwrites(lead, "channels", ch, overwrite("member", b.UserID, disconnect, 0)), http.StatusForbidden)
	wantStatus(t, "category role overwrite", putOverwrites(lead, "categories", cat, overwrite("role", botRole, disconnect, 0)), http.StatusForbidden)
	wantStatus(t, "category member overwrite", putOverwrites(lead, "categories", cat, overwrite("member", b.UserID, disconnect, 0)), http.StatusForbidden)
	wantStatus(t, "ordinary allow is fine", putOverwrites(lead, "channels", ch, overwrite("role", botRole, perm.ViewChannel|perm.SendMessages, 0)), http.StatusNoContent)

	// A role no bot holds may carry it; giving that role to a bot is then the Owner's.
	wantStatus(t, "loose role", putOverwrites(lead, "channels", ch,
		overwrite("role", botRole, perm.ViewChannel|perm.SendMessages, 0), overwrite("role", loose, disconnect, 0)), http.StatusNoContent)
	wantStatus(t, "assign it", lead.put(path+"/roles", map[string]any{"role_ids": []uuid.UUID{botRole, loose}}), http.StatusForbidden)

	// The Owner may, and the bot is then the Owner's to manage.
	wantStatus(t, "owner assigns", owner.put(path+"/roles", map[string]any{"role_ids": []uuid.UUID{botRole, loose}}), http.StatusOK)
	var acc bot.Access
	owner.get(path+"/access").decode(t, &acc)
	if !acc.Elevated {
		t.Fatalf("channel-level elevation not flagged: %+v", acc)
	}
	wantStatus(t, "administrator cannot mint", lead.post(path+"/tokens", map[string]any{}), http.StatusForbidden)
	wantStatus(t, "owner member overwrite", putOverwrites(owner, "channels", ch, overwrite("member", b.UserID, disconnect, 0)), http.StatusNoContent)
}

// The invariant is about the outcome, not the edit: removing a deny or moving
// a role can make a bot elevated without allowing anything new.
func TestOnlyTheOwnerCanMakeABotElevatedByAnyEdit(t *testing.T) {
	a := newApp(t, false)
	owner := a.seedAdmin()
	lead := a.register(owner, "lead")
	admins := createRole(t, owner, "Admins", perm.Administrator)
	assignRoles(t, owner, lead, admins)
	botRole := createRole(t, owner, "Plain bot", perm.SendMessages)
	cat := func() uuid.UUID {
		res := owner.post("/api/admin/categories", map[string]any{"name": "Cat"})
		var c struct{ ID uuid.UUID }
		res.decode(t, &c)
		return c.ID
	}()
	var ch uuid.UUID
	{
		res := owner.post("/api/admin/channels", map[string]any{"name": "ops", "type": "text", "category_id": cat})
		wantStatus(t, "channel in category", res, http.StatusCreated)
		var c struct{ ID uuid.UUID }
		res.decode(t, &c)
		ch = c.ID
	}
	b := createBot(t, owner, "echo")
	wantStatus(t, "bot role", owner.put("/api/admin/bots/"+b.ID.String()+"/roles", map[string]any{"role_ids": []uuid.UUID{botRole}}), http.StatusOK)

	// The Owner allows it on the category but denies it for the bot in the channel,
	// so the bot is not elevated anywhere.
	disconnect := perm.ViewChannel | perm.DisconnectMembers
	wantStatus(t, "category allow", putOverwrites(owner, "categories", cat, overwrite("role", botRole, disconnect, 0)), http.StatusNoContent)
	wantStatus(t, "member deny", putOverwrites(owner, "channels", ch, overwrite("member", b.UserID, 0, perm.DisconnectMembers)), http.StatusNoContent)
	var acc bot.Access
	owner.get("/api/admin/bots/"+b.ID.String()+"/access").decode(t, &acc)
	if acc.Elevated {
		t.Fatalf("denied in the channel: %+v", acc)
	}

	// An Administrator lifts the deny: the bot would become elevated, no allow bit involved.
	wantStatus(t, "lift the deny", putOverwrites(lead, "channels", ch), http.StatusForbidden)
	// Taking the category's allow away makes the bot less powerful, which anyone may do.
	wantStatus(t, "clear the category", putOverwrites(lead, "categories", cat), http.StatusNoContent)
	wantStatus(t, "restore it as Owner", putOverwrites(owner, "categories", cat, overwrite("role", botRole, disconnect, 0)), http.StatusNoContent)
	// Reordering roles grants nothing and stays possible.
	res := lead.get("/api/roles")
	var all []perm.Role
	res.decode(t, &all)
	ids := []uuid.UUID{}
	for _, r := range all {
		if !r.Everyone {
			ids = append(ids, r.ID)
		}
	}
	wantStatus(t, "reorder", lead.put("/api/admin/roles/order", map[string]any{"role_ids": ids}), http.StatusNoContent)
	// The Owner may.
	wantStatus(t, "owner lifts the deny", putOverwrites(owner, "channels", ch), http.StatusNoContent)
}

// A bot never gains channel access its manager lacks, whichever way the
// change is made, and the manager does not even see such channels in its
// access view.
func TestBotManagerCannotGrantChannelsTheyCannotSee(t *testing.T) {
	a := newApp(t, false)
	owner := a.seedAdmin()
	open := a.createChannel(owner, "open", "text")
	secret := a.createChannel(owner, "staff-only", "text")
	max := a.register(owner, "max")
	// Roles created later sit lower: max's role first, the bot's after it.
	maker := createRole(t, owner, "Bot makers", perm.ManageBots|perm.ManageChannels|perm.SendMessages)
	reader := createRole(t, owner, "Reader", perm.SendMessages)
	assignRoles(t, owner, max, maker)
	wantStatus(t, "hide the secret", putOverwrites(owner, "channels", secret,
		overwrite("role", everyoneID(t, owner), 0, perm.ViewChannel), overwrite("role", reader, perm.ViewChannel, 0)), http.StatusNoContent)
	wantStatus(t, "open for the reader", putOverwrites(owner, "channels", open, overwrite("role", reader, perm.ViewChannel, 0)), http.StatusNoContent)
	b := createBot(t, max, "janitor")
	path := "/api/admin/bots/" + b.ID.String()

	// The reader role would open the secret channel to the bot.
	wantStatus(t, "role with channels max cannot see", max.put(path+"/roles", map[string]any{"role_ids": []uuid.UUID{reader}}), http.StatusForbidden)
	// Allowing the bot itself in the secret channel is out of max's reach too.
	wantStatus(t, "member overwrite", putOverwrites(max, "channels", secret, overwrite("member", b.UserID, perm.ViewChannel, 0)), http.StatusNotFound)
	// The Owner may, and max still manages the bot but does not see the channel.
	wantStatus(t, "owner gives the role", owner.put(path+"/roles", map[string]any{"role_ids": []uuid.UUID{reader}}), http.StatusOK)
	res := max.get(path + "/access")
	wantStatus(t, "max reads the access", res, http.StatusOK)
	var acc bot.Access
	res.decode(t, &acc)
	for _, c := range acc.Channels {
		if c.ChannelID == secret || c.Name == "staff-only" {
			t.Fatalf("a channel the caller cannot see is listed: %s", res.body)
		}
	}
	if len(acc.Channels) != 1 || acc.Channels[0].ChannelID != open {
		t.Fatalf("access: %s", res.body)
	}
	owner.get(path+"/access").decode(t, &acc)
	if len(acc.Channels) != 2 {
		t.Fatalf("the Owner sees both: %+v", acc.Channels)
	}
}
