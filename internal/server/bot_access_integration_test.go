//go:build integration

package server

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/bot"
	"github.com/msch128/mnema-talk/internal/perm"
)

const wantAll = perm.ViewChannel | perm.SendMessages | perm.AddReactions | perm.ManageMessages

func accessBody(mode string, keys []string, chans ...uuid.UUID) map[string]any {
	if chans == nil {
		chans = []uuid.UUID{}
	}
	return map[string]any{"access": mode, "permissions": keys, "channel_ids": chans}
}

func installAll(t *testing.T, c *client, name string, keys []string, chans ...uuid.UUID) bot.InstallResponse {
	t.Helper()
	body := installBody(testManifest(name), keys, nil, chans...)
	body["access"] = "all"
	return installBot(t, c, body)
}

func accessOf(t *testing.T, c *client, b bot.Bot) bot.Access {
	t.Helper()
	var acc bot.Access
	res := c.get("/api/admin/bots/" + b.ID.String() + "/access")
	wantStatus(t, "access", res, http.StatusOK)
	res.decode(t, &acc)
	return acc
}

func ownPerms(t *testing.T, c *client, b bot.Bot) perm.Set {
	t.Helper()
	role, ok := ownRoleOf(t, c, b.UserID)
	if !ok {
		t.Fatal("no own role")
	}
	return role.Permissions
}

var allKeys = []string{"view_channel", "send_messages", "add_reactions", "manage_messages"}

func TestInstallWithAccessToAllChannels(t *testing.T) {
	a := newApp(t, false)
	owner := a.seedAdmin()
	open := a.createChannel(owner, "open", "text")
	other := a.createChannel(owner, "other", "text")
	staff := privateChannel(t, a, owner, "staff")

	out := installAll(t, owner, "Mnemo", allKeys)
	b := out.Bot
	if got := ownPerms(t, owner, b); got != wantAll {
		t.Fatalf("own role holds %b, want %b", got, wantAll)
	}
	if ows := ownRoleOverwrites(t, a, b.UserID); len(ows) != 0 {
		t.Fatalf("overwrites: %v", ows)
	}
	acc := accessOf(t, owner, b)
	if acc.Mode != "all" || acc.Granted != wantAll || acc.Elevated || acc.Permissions != wantAll || len(acc.SelectedChannelIDs) != 0 {
		t.Fatalf("access: %+v", acc)
	}
	bc := &botClient{t: t, a: a, token: out.Token.Secret, user: b.UserID}
	if seen := botChannels(t, bc); !slices.Equal(seen, sortedIDs(open, other)) {
		t.Fatalf("bot sees %v; the private channel must stay hidden", seen)
	}
	wantStatus(t, "private channel", bc.msgs(staff), http.StatusNotFound)
	if n := a.count(`SELECT COUNT(*) FROM audit_log WHERE action = 'bot.installed' AND details->>'access' = 'all'`); n != 1 {
		t.Fatalf("audit entries: %d", n)
	}

	// A private channel needs an explicit choice.
	with := installAll(t, owner, "Mnemo2", allKeys, staff)
	if ows := ownRoleOverwrites(t, a, with.Bot.UserID); len(ows) != 1 || ows[staff] != wantAll {
		t.Fatalf("overwrites: %v", ows)
	}
	wc := &botClient{t: t, a: a, token: with.Token.Secret, user: with.Bot.UserID}
	if seen := botChannels(t, wc); !slices.Equal(seen, sortedIDs(open, other, staff)) {
		t.Fatalf("bot with the private channel sees %v", seen)
	}
}

func TestInstallWithAccessToChannelsStaysAsItWas(t *testing.T) {
	a := newApp(t, false)
	owner := a.seedAdmin()
	open := a.createChannel(owner, "open", "text")
	a.createChannel(owner, "other", "text")

	for _, access := range []string{"", "channels"} {
		body := installBody(testManifest("Mnemo"+access), allKeys, nil, open)
		if access != "" {
			body["access"] = access
		}
		b := installBot(t, owner, body).Bot
		if ows := ownRoleOverwrites(t, a, b.UserID); len(ows) != 1 || ows[open] != wantAll {
			t.Fatalf("access %q: overwrites %v", access, ows)
		}
		if got := ownPerms(t, owner, b); got != 0 {
			t.Fatalf("access %q: server-wide %b", access, got)
		}
		if acc := accessOf(t, owner, b); acc.Mode != "channels" || acc.Granted != wantAll || !slices.Equal(acc.SelectedChannelIDs, []uuid.UUID{open}) {
			t.Fatalf("access %q: %+v", access, acc)
		}
	}
	body := installBody(testManifest("Bad"), allKeys, nil)
	body["access"] = "everything"
	wantStatus(t, "unknown access", owner.post("/api/admin/bots/install", body), http.StatusBadRequest)
}

func TestSwitchingBetweenAllAndSelectedChannels(t *testing.T) {
	a := newApp(t, false)
	owner := a.seedAdmin()
	open := a.createChannel(owner, "open", "text")
	other := a.createChannel(owner, "other", "text")
	staff := privateChannel(t, a, owner, "staff")
	b := installAll(t, owner, "Mnemo", allKeys).Bot
	path := "/api/admin/bots/" + b.ID.String() + "/access"
	bc := &botClient{t: t, a: a}
	sees := func(want ...uuid.UUID) {
		t.Helper()
		out := installTokenOf(t, owner, b)
		bc.token, bc.user = out, b.UserID
		if seen := botChannels(t, bc); !slices.Equal(seen, sortedIDs(want...)) {
			t.Fatalf("bot sees %v, want %v", seen, sortedIDs(want...))
		}
	}
	sees(open, other)

	// Back to selected channels: the server-wide grants go, overwrites appear.
	wantStatus(t, "to channels", owner.put(path, accessBody("channels", allKeys, open, staff)), http.StatusOK)
	if got := ownPerms(t, owner, b); got != 0 {
		t.Fatalf("server-wide after switching back: %b", got)
	}
	if ows := ownRoleOverwrites(t, a, b.UserID); len(ows) != 2 || ows[open] != wantAll || ows[staff] != wantAll {
		t.Fatalf("overwrites: %v", ows)
	}
	if acc := accessOf(t, owner, b); acc.Mode != "channels" || acc.Permissions != 0 {
		t.Fatalf("access: %+v", acc)
	}
	sees(open, staff)

	// Fewer channels and fewer permissions replace what was there.
	wantStatus(t, "narrower", owner.put(path, accessBody("channels", []string{"view_channel"}, open)), http.StatusOK)
	if ows := ownRoleOverwrites(t, a, b.UserID); len(ows) != 1 || ows[open] != perm.ViewChannel {
		t.Fatalf("overwrites: %v", ows)
	}
	sees(open)

	// To all channels: server-wide again; the overwrite of a public channel is gone.
	wantStatus(t, "to all", owner.put(path, accessBody("all", allKeys)), http.StatusOK)
	if got := ownPerms(t, owner, b); got != wantAll {
		t.Fatalf("own role after switching to all: %b", got)
	}
	if ows := ownRoleOverwrites(t, a, b.UserID); len(ows) != 0 {
		t.Fatalf("overwrites: %v", ows)
	}
	sees(open, other)

	// Nothing but channel permissions can be asked for.
	for _, key := range []string{"administrator", "manage_channels", "ban_members", "manage_bots"} {
		res := owner.put(path, accessBody("all", []string{"view_channel", key}))
		wantStatus(t, key, res, http.StatusBadRequest)
	}
	wantStatus(t, "unknown access", owner.put(path, accessBody("some", allKeys)), http.StatusBadRequest)
	wantStatus(t, "hidden channel", owner.put(path, accessBody("channels", allKeys, uuid.New())), http.StatusNotFound)
	if got := ownPerms(t, owner, b); got != wantAll {
		t.Fatalf("a refused change changed the role: %b", got)
	}
	if n := a.count(`SELECT COUNT(*) FROM audit_log WHERE action = 'bot.access' AND target_id = $1`, b.ID.String()); n != 3 {
		t.Fatalf("audit entries: %d", n)
	}
}

// installTokenOf issues a fresh token for the bot, to speak as it.
func installTokenOf(t *testing.T, c *client, b bot.Bot) string {
	t.Helper()
	return issueToken(t, c, b.ID, map[string]any{"name": uuid.NewString()[:8]}).Secret
}

func TestAccessToAllChannelsIsNotAWayToEscalate(t *testing.T) {
	a := newApp(t, false)
	owner := a.seedAdmin()
	open := a.createChannel(owner, "open", "text")
	muted := a.createChannel(owner, "muted", "text")
	staff := privateChannel(t, a, owner, "staff")
	maker := a.register(owner, "maker")
	makers := createRole(t, owner, "Bot makers", perm.ManageBots)
	assignRoles(t, owner, maker, makers)
	plain := a.register(owner, "plain")

	// The maker lacks Manage messages everywhere.
	res := maker.post("/api/admin/bots/install", func() map[string]any {
		b := installBody(testManifest("x"), allKeys, nil)
		b["access"] = "all"
		return b
	}())
	wantStatus(t, "manage messages for all", res, http.StatusForbidden)
	if !strings.Contains(string(res.body), "manage messages") {
		t.Fatalf("the refusal does not name the permission: %s", res.body)
	}
	if n := a.count(`SELECT COUNT(*) FROM bots`); n != 0 {
		t.Fatalf("a refused install left %d bots", n)
	}

	// What the maker holds in every channel they see is fine; the private
	// channel stays out of the bot's reach.
	keys := []string{"view_channel", "send_messages"}
	ok := installAll(t, maker, "Mnemo", keys)
	bc := &botClient{t: t, a: a, token: ok.Token.Secret, user: ok.Bot.UserID}
	if seen := botChannels(t, bc); !slices.Equal(seen, sortedIDs(open, muted)) {
		t.Fatalf("bot sees %v", seen)
	}

	// Switching an existing bot is bound by the same rules, and a private
	// channel the maker cannot view is not theirs to list.
	path := "/api/admin/bots/" + ok.Bot.ID.String() + "/access"
	wantStatus(t, "existing bot, private", maker.put(path, accessBody("channels", []string{"view_channel"}, staff)), http.StatusNotFound)
	wantStatus(t, "existing bot, administrator", maker.put(path, accessBody("channels", []string{"administrator"}, open)), http.StatusBadRequest)
	wantStatus(t, "plain member", plain.put(path, accessBody("channels", keys, open)), http.StatusForbidden)

	// The maker cannot send in "muted": a bot holding Send everywhere would
	// have more there than they do.
	wantStatus(t, "deny send", putOverwrites(owner, "channels", muted, overwrite("role", makers, 0, perm.SendMessages)), http.StatusNoContent)
	res = maker.post("/api/admin/bots/install", func() map[string]any {
		b := installBody(testManifest("y"), keys, nil)
		b["access"] = "all"
		return b
	}())
	wantStatus(t, "send where the maker cannot", res, http.StatusForbidden)

	wantStatus(t, "existing bot, all", maker.put(path, accessBody("all", keys)), http.StatusForbidden)
	if got := ownPerms(t, owner, ok.Bot); got != perm.ViewChannel|perm.SendMessages {
		t.Fatalf("the bot's role changed: %b", got)
	}
}

func TestMakerCannotChangeABotThatReachesAChannelTheyCannotSee(t *testing.T) {
	a := newApp(t, false)
	owner := a.seedAdmin()
	open := a.createChannel(owner, "open", "text")
	staff := privateChannel(t, a, owner, "staff")
	maker := a.register(owner, "maker")
	assignRoles(t, owner, maker, createRole(t, owner, "Bot makers", perm.ManageBots))
	keys := []string{"view_channel", "send_messages"}
	b := installAll(t, owner, "Mnemo", keys, staff).Bot

	path := "/api/admin/bots/" + b.ID.String() + "/access"
	wantStatus(t, "to channels", maker.put(path, accessBody("channels", keys, open)), http.StatusForbidden)
	if ows := ownRoleOverwrites(t, a, b.UserID); len(ows) != 1 || ows[staff] != perm.ViewChannel|perm.SendMessages {
		t.Fatalf("overwrites: %v", ows)
	}
	// The listing shows the maker no channel they cannot see.
	var acc bot.Access
	if res := maker.get(path); res.status == http.StatusOK {
		res.decode(t, &acc)
		if slices.Contains(acc.SelectedChannelIDs, staff) {
			t.Fatalf("the maker was told about a channel they cannot see: %+v", acc)
		}
	}
}

func TestAccessChoiceNeedsAuthorityOverTheBot(t *testing.T) {
	a := newApp(t, false)
	owner := a.seedAdmin()
	open := a.createChannel(owner, "open", "text")
	senior := createRole(t, owner, "Seniors", 0)
	maker := a.register(owner, "maker")
	assignRoles(t, owner, maker, createRole(t, owner, "Bot makers", perm.ManageBots))
	keys := []string{"view_channel", "send_messages"}
	b := installAll(t, owner, "Mnemo", keys).Bot
	wantStatus(t, "senior role for the bot", owner.put("/api/admin/bots/"+b.ID.String()+"/roles", map[string]any{"role_ids": []uuid.UUID{senior}}), http.StatusOK)

	// The bot's highest role is not below the maker's.
	wantStatus(t, "maker changes a senior bot", maker.put("/api/admin/bots/"+b.ID.String()+"/access", accessBody("channels", keys, open)), http.StatusForbidden)
	if got := ownPerms(t, owner, b); got != perm.ViewChannel|perm.SendMessages {
		t.Fatalf("the bot's role changed: %b", got)
	}
}

func TestSwitchingKeepsOverwritesInChannelsTheCallerCannotView(t *testing.T) {
	a := newApp(t, false)
	owner := a.seedAdmin()
	open := a.createChannel(owner, "open", "text")
	staff := privateChannel(t, a, owner, "staff")
	maker := a.register(owner, "maker")
	assignRoles(t, owner, maker, createRole(t, owner, "Bot makers", perm.ManageBots))
	keys := []string{"view_channel", "send_messages"}
	b := installAll(t, owner, "Mnemo", keys).Bot
	// An allow of Send in the private channel, without View: the bot does not
	// reach it, so the maker may manage the bot, but it is not theirs to drop.
	role, _ := ownRoleOf(t, owner, b.UserID)
	wantStatus(t, "own overwrite", putOverwrites(owner, "channels", staff,
		overwrite("role", everyoneID(t, owner), 0, perm.ViewChannel), overwrite("role", role.ID, perm.SendMessages, 0)), http.StatusNoContent)

	wantStatus(t, "to channels", maker.put("/api/admin/bots/"+b.ID.String()+"/access", accessBody("channels", keys, open)), http.StatusOK)
	ows := ownRoleOverwrites(t, a, b.UserID)
	if len(ows) != 2 || ows[open] != perm.ViewChannel|perm.SendMessages || ows[staff] != perm.SendMessages {
		t.Fatalf("overwrites: %v", ows)
	}
}

func TestAccessToAllChannelsKeepsAPrivateCategoryPrivate(t *testing.T) {
	a := newApp(t, false)
	owner := a.seedAdmin()
	open := a.createChannel(owner, "open", "text")
	var cat struct{ ID uuid.UUID }
	owner.post("/api/admin/categories", map[string]any{"name": "Staff"}).decode(t, &cat)
	wantStatus(t, "private category", putOverwrites(owner, "categories", cat.ID, overwrite("role", everyoneID(t, owner), 0, perm.ViewChannel)), http.StatusNoContent)
	wantStatus(t, "channel in the category", owner.post("/api/admin/channels", map[string]any{"name": "inside", "type": "text", "category_id": cat.ID}), http.StatusCreated)

	out := installAll(t, owner, "Mnemo", allKeys)
	bc := &botClient{t: t, a: a, token: out.Token.Secret, user: out.Bot.UserID}
	if seen := botChannels(t, bc); !slices.Equal(seen, []uuid.UUID{open}) {
		t.Fatalf("bot sees %v", seen)
	}
	var tree struct {
		Categories []struct{ Name string } `json:"categories"`
	}
	bc.get(bot.BasePath+"/channels").decode(t, &tree)
	if len(tree.Categories) != 0 {
		t.Fatalf("the private category is listed: %+v", tree.Categories)
	}
}

func TestSelectedChannelsReplaceCategoryOverwritesAndReportDifferences(t *testing.T) {
	a := newApp(t, false)
	owner := a.seedAdmin()
	one := a.createChannel(owner, "one", "text")
	two := a.createChannel(owner, "two", "text")
	var cat struct{ ID uuid.UUID }
	owner.post("/api/admin/categories", map[string]any{"name": "Team"}).decode(t, &cat)
	wantStatus(t, "channel in the category", owner.post("/api/admin/channels", map[string]any{"name": "inside", "type": "text", "category_id": cat.ID}), http.StatusCreated)
	b := installAll(t, owner, "Mnemo", allKeys).Bot
	role, _ := ownRoleOf(t, owner, b.UserID)
	path := "/api/admin/bots/" + b.ID.String() + "/access"
	wantStatus(t, "to channels", owner.put(path, accessBody("channels", []string{"view_channel"}, one)), http.StatusOK)

	// Different sets by hand: reported, and the shared set is what both allow.
	wantStatus(t, "by hand", putOverwrites(owner, "channels", two, overwrite("role", role.ID, perm.ViewChannel|perm.SendMessages, 0)), http.StatusNoContent)
	if acc := accessOf(t, owner, b); !acc.Mixed || acc.Granted != perm.ViewChannel {
		t.Fatalf("access: %+v", acc)
	}

	// An allow on the category is a way in too: choosing channels removes it.
	wantStatus(t, "category", putOverwrites(owner, "categories", cat.ID, overwrite("role", role.ID, perm.ViewChannel, 0)), http.StatusNoContent)
	bc := &botClient{t: t, a: a, token: installTokenOf(t, owner, b), user: b.UserID}
	if len(botChannels(t, bc)) != 3 {
		t.Fatalf("the category did not open its channel: %v", botChannels(t, bc))
	}
	wantStatus(t, "selected", owner.put(path, accessBody("channels", []string{"view_channel"}, one)), http.StatusOK)
	if seen := botChannels(t, bc); !slices.Equal(seen, []uuid.UUID{one}) {
		t.Fatalf("bot sees %v after choosing channels", seen)
	}
	if acc := accessOf(t, owner, b); acc.Mixed {
		t.Fatalf("still mixed: %+v", acc)
	}
}

func TestInstallWithAccessToAllNamesTheChannelWhereThePermissionIsLacking(t *testing.T) {
	a := newApp(t, false)
	owner := a.seedAdmin()
	a.createChannel(owner, "open", "text")
	muted := a.createChannel(owner, "muted", "text")
	maker := a.register(owner, "maker")
	makers := createRole(t, owner, "Bot makers", perm.ManageBots)
	assignRoles(t, owner, maker, makers)
	wantStatus(t, "deny send", putOverwrites(owner, "channels", muted, overwrite("role", makers, 0, perm.SendMessages)), http.StatusNoContent)
	body := installBody(testManifest("x"), []string{"view_channel", "send_messages"}, nil)
	body["access"] = "all"
	res := maker.post("/api/admin/bots/install", body)
	wantStatus(t, "send in one channel", res, http.StatusForbidden)
	if !strings.Contains(string(res.body), "send messages in #muted") {
		t.Fatalf("the refusal does not name the channel: %s", res.body)
	}
}
