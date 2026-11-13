//go:build integration

package server

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/bot"
	"github.com/msch128/mnema-talk/internal/chat/channels"
	"github.com/msch128/mnema-talk/internal/perm"
)

// testManifest is a manifest as a bot's README would publish it, with a field
// from the future that the server must ignore.
func testManifest(name string) map[string]any {
	return map[string]any{
		"manifest_version": 1,
		"name":             name,
		"description":      "Answers questions and keeps notes.",
		"homepage":         "https://example.com/mnemo",
		"avatar":           "data:image/png;base64,iVBORw0KGgo=",
		"intents":          []string{"GUILDS", "GUILD_MESSAGES", "GUILD_MEMBERS"},
		"permissions":      []string{"view_channel", "send_messages", "add_reactions", "manage_messages"},
		"commands": []map[string]any{{"name": "ask", "description": "Ask something",
			"options": []map[string]any{{"name": "question", "description": "Your question", "type": 3, "required": true}}}},
		"from_the_future": map[string]any{"x": 1},
	}
}

func installBody(m map[string]any, perms, intents []string, chans ...uuid.UUID) map[string]any {
	if chans == nil {
		chans = []uuid.UUID{}
	}
	return map[string]any{"manifest": m, "permissions": perms, "intents": intents, "channel_ids": chans}
}

func installBot(t *testing.T, c *client, body map[string]any) bot.InstallResponse {
	t.Helper()
	res := c.post("/api/admin/bots/install", body)
	if res.status != http.StatusCreated {
		t.Fatalf("install: %d %s", res.status, res.body)
	}
	var out bot.InstallResponse
	res.decode(t, &out)
	return out
}

// ownRoleOverwrites maps each channel with an overwrite of the bot's own role
// to what it allows.
func ownRoleOverwrites(t *testing.T, a *app, botUser uuid.UUID) map[uuid.UUID]perm.Set {
	t.Helper()
	rows, err := a.db.Query(context.Background(), `
		SELECT o.channel_id, o.allow FROM permission_overwrites o JOIN roles r ON r.id = o.role_id
		WHERE r.bot_user_id = $1`, botUser)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[uuid.UUID]perm.Set{}
	for rows.Next() {
		var ch *uuid.UUID
		var allow int64
		if err := rows.Scan(&ch, &allow); err != nil {
			t.Fatal(err)
		}
		if ch == nil {
			t.Fatal("an overwrite on a category")
		}
		out[*ch] = perm.Set(allow)
	}
	return out
}

// botChannels lists the channels a bot token sees through the bot API.
func botChannels(t *testing.T, c *botClient) []uuid.UUID {
	t.Helper()
	res := c.get(bot.BasePath + "/channels")
	wantStatus(t, "bot channels", res, http.StatusOK)
	var tree channels.ChannelHierarchy
	res.decode(t, &tree)
	var seen []uuid.UUID
	for _, ch := range tree.Uncategorized {
		seen = append(seen, ch.ID)
	}
	for _, cat := range tree.Categories {
		for _, ch := range cat.Channels {
			seen = append(seen, ch.ID)
		}
	}
	slices.SortFunc(seen, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
	return seen
}

func sortedIDs(ids ...uuid.UUID) []uuid.UUID {
	slices.SortFunc(ids, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
	return ids
}

// privateChannel makes a channel only the Owner (and roles given it) can view.
func privateChannel(t *testing.T, a *app, owner *client, name string) uuid.UUID {
	t.Helper()
	ch := a.createChannel(owner, name, "text")
	wantStatus(t, "private "+name, putOverwrites(owner, "channels", ch, overwrite("role", everyoneID(t, owner), 0, perm.ViewChannel)), http.StatusNoContent)
	return ch
}

func TestInstallFromManifestAsOwner(t *testing.T) {
	a := newApp(t, false)
	owner := a.seedAdmin()
	open := a.createChannel(owner, "open", "text")
	a.createChannel(owner, "other", "text")
	secret := privateChannel(t, a, owner, "staff")

	kept := []string{"view_channel", "send_messages", "manage_messages"}
	out := installBot(t, owner, installBody(testManifest("Mnemo"), kept, []string{"GUILDS", "GUILD_MESSAGES"}, open, secret, open))
	b := out.Bot
	if b.Name != "Mnemo" || b.Description != "Answers questions and keeps notes." || b.Homepage != "https://example.com/mnemo" {
		t.Fatalf("bot: %+v", b)
	}
	if b.AllowedIntents != bot.IntentGuilds|bot.IntentGuildMessages {
		t.Fatalf("intents: %b", b.AllowedIntents)
	}
	if !strings.HasPrefix(out.Token.Secret, bot.TokenPrefix) || out.Token.Name != "Mnemo" || out.Token.State != "active" {
		t.Fatalf("token: %+v", out.Token)
	}
	// The bot reads back with its profile; the secret is never listed.
	var got bot.Bot
	owner.get("/api/admin/bots/"+b.ID.String()).decode(t, &got)
	if got.Description != b.Description || got.Homepage != b.Homepage {
		t.Fatalf("read back: %+v", got)
	}
	if list := owner.get("/api/admin/bots/" + b.ID.String() + "/tokens"); strings.Contains(string(list.body), out.Token.Secret) {
		t.Fatal("the secret is listed")
	}

	// Overwrites of its own role in exactly the ticked channels, and nothing server-wide.
	want := perm.ViewChannel | perm.SendMessages | perm.ManageMessages
	ows := ownRoleOverwrites(t, a, b.UserID)
	if len(ows) != 2 || ows[open] != want || ows[secret] != want {
		t.Fatalf("overwrites: %v", ows)
	}
	role, ok := ownRoleOf(t, owner, b.UserID)
	if !ok || role.Permissions != 0 {
		t.Fatalf("own role: %+v %v", role, ok)
	}
	var acc bot.Access
	owner.get("/api/admin/bots/"+b.ID.String()+"/access").decode(t, &acc)
	if acc.Permissions != 0 || acc.Elevated {
		t.Fatalf("server-wide: %+v", acc)
	}

	// Commands registered as if the bot had sent them.
	var cmds []bot.Command
	owner.get("/api/admin/bots/"+b.ID.String()+"/commands").decode(t, &cmds)
	if len(cmds) != 1 || cmds[0].Name != "ask" || len(cmds[0].Options) != 1 {
		t.Fatalf("commands: %+v", cmds)
	}

	// The token works and sees exactly the ticked channels.
	bc := &botClient{t: t, a: a, token: out.Token.Secret, user: b.UserID}
	var self bot.Self
	res := bc.get(bot.BasePath + "/users/@me")
	wantStatus(t, "me", res, http.StatusOK)
	res.decode(t, &self)
	if self.BotID != b.ID {
		t.Fatalf("me: %s", res.body)
	}
	if seen := botChannels(t, bc); !slices.Equal(seen, sortedIDs(open, secret)) {
		t.Fatalf("bot sees %v", seen)
	}

	// One audit entry, without the secret.
	if n := a.count(`SELECT COUNT(*) FROM audit_log WHERE action = 'bot.installed' AND target_id = $1 AND target_name = 'Mnemo'`, b.ID.String()); n != 1 {
		t.Fatalf("audit entries: %d", n)
	}
	if n := a.count(`SELECT COUNT(*) FROM audit_log WHERE details::text LIKE '%' || $1 || '%'`, out.Token.Secret); n != 0 {
		t.Fatal("the secret reached the audit log")
	}
	if n := a.count(`SELECT COUNT(*) FROM audit_log WHERE action = 'bot.installed' AND details->'permissions' ? 'manage_messages'`); n != 1 {
		t.Fatal("the audit entry does not say what was granted")
	}
}

func TestInstallFromManifestAsManageBotsMember(t *testing.T) {
	a := newApp(t, false)
	owner := a.seedAdmin()
	open := a.createChannel(owner, "open", "text")
	secret := privateChannel(t, a, owner, "staff")
	maker := a.register(owner, "maker")
	assignRoles(t, owner, maker, createRole(t, owner, "Bot makers", perm.ManageBots))

	// A channel the installer cannot view is like one that does not exist.
	res := maker.post("/api/admin/bots/install", installBody(testManifest("Mnemo"), []string{"view_channel"}, nil, open, secret))
	wantStatus(t, "hidden channel", res, http.StatusNotFound)

	out := installBot(t, maker, installBody(testManifest("Mnemo"), []string{"view_channel", "send_messages"}, []string{"GUILDS"}, open))
	if ows := ownRoleOverwrites(t, a, out.Bot.UserID); len(ows) != 1 || ows[open] != perm.ViewChannel|perm.SendMessages {
		t.Fatalf("overwrites: %v", ows)
	}
	if out.Bot.CreatedBy == nil || *out.Bot.CreatedBy != maker.user.ID {
		t.Fatalf("created by: %v", out.Bot.CreatedBy)
	}
	bc := &botClient{t: t, a: a, token: out.Token.Secret, user: out.Bot.UserID}
	if seen := botChannels(t, bc); !slices.Equal(seen, []uuid.UUID{open}) {
		t.Fatalf("bot sees %v; the private channel must stay invisible", seen)
	}
	wantStatus(t, "private channel", bc.msgs(secret), http.StatusNotFound)
	// The installer may manage the bot they added, tokens included.
	issueToken(t, maker, out.Bot.ID, map[string]any{"name": "second"})

	// A bot added without a ticked channel sees nothing.
	none := installBot(t, maker, installBody(testManifest("Quiet"), []string{"view_channel"}, nil))
	if seen := botChannels(t, &botClient{t: t, a: a, token: none.Token.Secret}); len(seen) != 0 {
		t.Fatalf("a bot without channels sees %v", seen)
	}
}

// installCounts is what an install creates: bots, own roles, tokens, bot
// accounts and overwrites.
func installCounts(a *app) [5]int {
	return [5]int{
		a.count(`SELECT COUNT(*) FROM bots`),
		a.count(`SELECT COUNT(*) FROM roles WHERE bot_user_id IS NOT NULL`),
		a.count(`SELECT COUNT(*) FROM bot_tokens`),
		a.count(`SELECT COUNT(*) FROM users WHERE is_bot`),
		a.count(`SELECT COUNT(*) FROM permission_overwrites`),
	}
}

func TestInstallRefusalsCreateNothing(t *testing.T) {
	a := newApp(t, false)
	owner := a.seedAdmin()
	open := a.createChannel(owner, "open", "text")
	secret := privateChannel(t, a, owner, "staff")
	maker := a.register(owner, "maker")
	assignRoles(t, owner, maker, createRole(t, owner, "Bot makers", perm.ManageBots))
	plain := a.register(owner, "plain")
	peer := a.register(owner, "peer")
	var conv dmView
	maker.startDM(peer).decode(t, &conv)
	var cat struct{ ID uuid.UUID }
	owner.post("/api/admin/categories", map[string]any{"name": "Cat"}).decode(t, &cat)

	elevated := testManifest("x")
	elevated["permissions"] = []string{"view_channel", "administrator"}
	adminArea := testManifest("x")
	adminArea["permissions"] = []string{"manage_channels"}
	unknown := testManifest("x")
	unknown["permissions"] = []string{"view_channel", "rule_the_world"}
	keep := []string{"view_channel", "send_messages"}

	before := installCounts(a)
	for _, tc := range []struct {
		name   string
		who    *client
		body   map[string]any
		status int
		want   string
	}{
		{"administrator in the manifest", owner, installBody(elevated, keep, nil, open), http.StatusBadRequest, "administrator"},
		{"an admin-area permission in the manifest", owner, installBody(adminArea, nil, nil), http.StatusBadRequest, "manage_channels"},
		{"an unknown permission in the manifest", owner, installBody(unknown, keep, nil), http.StatusBadRequest, "rule_the_world"},
		{"a kept permission not in the manifest", maker, installBody(testManifest("x"), []string{"attach_files"}, nil, open), http.StatusBadRequest, "attach_files"},
		{"a kept intent not in the manifest", maker, installBody(testManifest("x"), keep, []string{"GUILD_MESSAGE_REACTIONS"}, open), http.StatusBadRequest, "GUILD_MESSAGE_REACTIONS"},
		{"a channel the installer cannot view", maker, installBody(testManifest("x"), keep, nil, open, secret), http.StatusNotFound, ""},
		{"a category", maker, installBody(testManifest("x"), keep, nil, cat.ID), http.StatusNotFound, ""},
		{"a direct message conversation", maker, installBody(testManifest("x"), keep, nil, conv.ID), http.StatusNotFound, ""},
		{"an unknown id", owner, installBody(testManifest("x"), keep, nil, uuid.New()), http.StatusNotFound, ""},
		{"a permission the installer lacks in a ticked channel", maker, installBody(testManifest("x"), []string{"view_channel", "manage_messages"}, nil, open), http.StatusForbidden, "manage messages in #open"},
		{"a member without Manage bots", plain, installBody(testManifest("x"), keep, nil, open), http.StatusForbidden, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := tc.who.post("/api/admin/bots/install", tc.body)
			wantStatus(t, tc.name, res, tc.status)
			if !strings.Contains(string(res.body), tc.want) {
				t.Fatalf("the message does not name %q: %s", tc.want, res.body)
			}
			if after := installCounts(a); after != before {
				t.Fatalf("a refused install left something behind: %v, was %v", after, before)
			}
		})
	}
	if n := a.count(`SELECT COUNT(*) FROM audit_log WHERE action = 'bot.installed'`); n != 0 {
		t.Fatalf("refused installs were logged: %d", n)
	}
}

func TestCheckManifestAnswersItNormalisedWithoutSideEffects(t *testing.T) {
	a := newApp(t, false)
	owner := a.seedAdmin()
	plain := a.register(owner, "plain")
	before := installCounts(a)

	res := owner.post("/api/admin/bots/manifests/check", testManifest("  Mnemo "))
	wantStatus(t, "check", res, http.StatusOK)
	var got bot.CheckedManifest
	res.decode(t, &got)
	if got.Name != "Mnemo" || len(got.Intents) != 3 || got.Intents[0].Name != "GUILDS" || got.Intents[0].Bit != bot.IntentGuilds {
		t.Fatalf("checked: %s", res.body)
	}
	if len(got.Permissions) != 4 || got.Permissions[0].Key != "view_channel" || got.Permissions[0].Name == "" || len(got.Commands) != 1 {
		t.Fatalf("checked: %s", res.body)
	}
	if strings.Contains(string(res.body), "avatar") || strings.Contains(string(res.body), "from_the_future") {
		t.Fatalf("ignored fields came back: %s", res.body)
	}
	bad := testManifest("x")
	bad["permissions"] = []string{"ban_members"}
	res = owner.post("/api/admin/bots/manifests/check", bad)
	wantStatus(t, "elevated", res, http.StatusBadRequest)
	if !strings.Contains(string(res.body), "ban_members") {
		t.Fatalf("the refusal does not name the permission: %s", res.body)
	}
	wantStatus(t, "plain member", plain.post("/api/admin/bots/manifests/check", testManifest("x")), http.StatusForbidden)
	if after := installCounts(a); after != before {
		t.Fatalf("a check changed something: %v, was %v", after, before)
	}
}
