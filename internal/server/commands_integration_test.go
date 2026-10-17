//go:build integration

package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/bot"
	"github.com/msch128/mnema-talk/internal/gateway"
	"github.com/msch128/mnema-talk/internal/perm"
)

func (b *botClient) putJSON(p string, v any) response { return b.req(http.MethodPut, p, v, nil) }

var pingCommand = map[string]any{
	"name": "ping", "description": "Replies with pong",
	"options": []map[string]any{
		{"name": "text", "description": "What to say", "type": bot.OptionString, "required": true},
		{"name": "times", "description": "How often", "type": bot.OptionInteger},
		{"name": "loud", "description": "Shout", "type": bot.OptionBoolean},
		{"name": "who", "description": "A member", "type": bot.OptionUser},
		{"name": "where", "description": "A channel", "type": bot.OptionChannel},
	},
}

func commandsOf(t *testing.T, res response) []bot.Command {
	t.Helper()
	var list []bot.Command
	res.decode(t, &list)
	return list
}

func TestBotCommandRegistration(t *testing.T) {
	f := newBotFixture(t, false)
	f.a.router.Bots.SetLimits(1000, 1000)
	c := f.client
	base := bot.BasePath + "/commands"

	res := c.post(base, pingCommand)
	wantStatus(t, "create", res, http.StatusOK)
	var cmd bot.Command
	res.decode(t, &cmd)
	if cmd.Name != "ping" || len(cmd.Options) != 5 || cmd.Disabled || cmd.BotID != f.bot.ID {
		t.Fatalf("command: %s", res.body)
	}
	wantStatus(t, "update by name", c.post(base, map[string]any{"name": "ping", "description": "changed"}), http.StatusOK)
	list := commandsOf(t, c.get(base))
	if len(list) != 1 || list[0].Description != "changed" || len(list[0].Options) != 0 {
		t.Fatalf("list: %+v", list)
	}

	// Validation.
	for name, body := range map[string]map[string]any{
		"uppercase":       {"name": "Ping", "description": "x"},
		"space":           {"name": "a b", "description": "x"},
		"no description":  {"name": "x", "description": " "},
		"bad option type": {"name": "x", "description": "x", "options": []map[string]any{{"name": "a", "description": "x", "type": 99}}},
		"duplicate option": {"name": "x", "description": "x", "options": []map[string]any{
			{"name": "a", "description": "x", "type": 3}, {"name": "a", "description": "x", "type": 3}}},
		"required after optional": {"name": "x", "description": "x", "options": []map[string]any{
			{"name": "a", "description": "x", "type": 3}, {"name": "b", "description": "x", "type": 3, "required": true}}},
		"unknown field": {"name": "x", "description": "x", "bogus": 1},
	} {
		wantStatus(t, name, c.post(base, body), http.StatusBadRequest)
	}
	many := make([]map[string]any, bot.MaxOptions+1)
	for i := range many {
		many[i] = map[string]any{"name": "o" + strings.Repeat("a", i), "description": "x", "type": 3}
	}
	wantStatus(t, "too many options", c.post(base, map[string]any{"name": "x", "description": "x", "options": many}), http.StatusBadRequest)

	// An admin's switch-off survives the bot registering the command again.
	adminList := f.owner.get(f.botPath + "/commands")
	wantStatus(t, "admin list", adminList, http.StatusOK)
	id := commandsOf(t, adminList)[0].ID
	wantStatus(t, "disable", f.owner.patch(f.botPath+"/commands/"+id.String(), map[string]any{"disabled": true}), http.StatusOK)
	wantStatus(t, "re-register", c.post(base, map[string]any{"name": "ping", "description": "again"}), http.StatusOK)
	if got := commandsOf(t, c.get(base)); !got[0].Disabled || got[0].Description != "again" {
		t.Fatalf("disabled flag lost: %+v", got)
	}

	// Bulk overwrite keeps the flag, adds and removes.
	res = c.putJSON(base, []map[string]any{{"name": "ping", "description": "bulk"}, {"name": "help", "description": "Help"}})
	wantStatus(t, "replace", res, http.StatusOK)
	got := commandsOf(t, res)
	if len(got) != 2 || got[0].Name != "help" || got[1].Name != "ping" || !got[1].Disabled {
		t.Fatalf("replace: %s", res.body)
	}
	wantStatus(t, "replace with duplicates", c.putJSON(base, []map[string]any{{"name": "a", "description": "x"}, {"name": "a", "description": "x"}}), http.StatusBadRequest)
	wantStatus(t, "replace with invalid", c.putJSON(base, []map[string]any{{"name": "A", "description": "x"}}), http.StatusBadRequest)
	tooMany := make([]map[string]any, bot.MaxCommands+1)
	for i := range tooMany {
		tooMany[i] = map[string]any{"name": fmt.Sprintf("c%d", i), "description": "x"}
	}
	wantStatus(t, "replace over the limit", c.putJSON(base, tooMany), http.StatusBadRequest)
	res = c.putJSON(base, []map[string]any{})
	if got := commandsOf(t, res); len(got) != 0 {
		t.Fatalf("empty replace: %s", res.body)
	}

	// The limit for single registrations.
	for i := 0; i < bot.MaxCommands; i++ {
		wantStatus(t, "fill", c.post(base, map[string]any{"name": fmt.Sprintf("c%d", i), "description": "x"}), http.StatusOK)
	}
	wantStatus(t, "over the limit", c.post(base, map[string]any{"name": "one-too-many", "description": "x"}), http.StatusConflict)

	// Delete.
	list = commandsOf(t, c.get(base))
	wantStatus(t, "delete", c.delete(base+"/"+list[0].ID.String()), http.StatusNoContent)
	wantStatus(t, "delete again", c.delete(base+"/"+list[0].ID.String()), http.StatusNotFound)
	wantStatus(t, "delete bad id", c.delete(base+"/x"), http.StatusBadRequest)
	// Admin delete.
	wantStatus(t, "admin delete", f.owner.delete(f.botPath+"/commands/"+list[1].ID.String()), http.StatusNoContent)
	wantStatus(t, "admin delete again", f.owner.delete(f.botPath+"/commands/"+list[1].ID.String()), http.StatusNotFound)
	wantStatus(t, "admin patch unknown", f.owner.patch(f.botPath+"/commands/"+uuid.NewString(), map[string]any{"disabled": true}), http.StatusNotFound)
}

func channelCommands(t *testing.T, c *client, ch uuid.UUID) []bot.ChannelCommand {
	t.Helper()
	res := c.get("/api/channels/" + ch.String() + "/commands")
	wantStatus(t, "channel commands", res, http.StatusOK)
	var out []bot.ChannelCommand
	res.decode(t, &out)
	return out
}

// Commands are visible where the bot can view and send, minus what an admin switched off.
func TestSlashCommandsAreScopedToTheBotsChannels(t *testing.T) {
	f := gwFixture(t)
	max := f.a.register(f.owner, "max")
	wantStatus(t, "register", f.client.post(bot.BasePath+"/commands", pingCommand), http.StatusOK)

	got := channelCommands(t, max, f.open)
	if len(got) != 1 || got[0].Name != "ping" || got[0].BotName != "helper" || len(got[0].Options) != 5 {
		t.Fatalf("commands: %+v", got)
	}
	if got := channelCommands(t, max, f.other); len(got) != 0 {
		t.Fatalf("a channel the bot cannot use lists its commands: %+v", got)
	}
	// Hidden channels look like missing ones.
	secret := f.a.createChannel(f.owner, "staff-only", "text")
	staff := createRole(t, f.owner, "Staff", 0)
	wantStatus(t, "private", putOverwrites(f.owner, "channels", secret,
		overwrite("role", everyoneID(t, f.owner), 0, perm.ViewChannel), overwrite("role", staff, perm.ViewChannel, 0)), http.StatusNoContent)
	wantStatus(t, "hidden", max.get("/api/channels/"+secret.String()+"/commands"), http.StatusNotFound)

	// Taking Send messages from the bot's role removes its commands from the channel.
	wantStatus(t, "no send", f.owner.patch("/api/admin/roles/"+f.roleID.String(), map[string]any{"permissions": perm.AddReactions}), http.StatusOK)
	if got := channelCommands(t, max, f.open); len(got) != 0 {
		t.Fatalf("bot cannot send any more: %+v", got)
	}
	wantStatus(t, "restore", f.owner.patch("/api/admin/roles/"+f.roleID.String(), map[string]any{"permissions": perm.SendMessages}), http.StatusOK)
	if got := channelCommands(t, max, f.open); len(got) != 1 {
		t.Fatalf("restored: %+v", got)
	}

	// An admin can switch a command off, and the bot as a whole.
	id := got[0].ID
	wantStatus(t, "disable command", f.owner.patch(f.botPath+"/commands/"+id.String(), map[string]any{"disabled": true}), http.StatusOK)
	if got := channelCommands(t, max, f.open); len(got) != 0 {
		t.Fatalf("disabled command listed: %+v", got)
	}
	wantStatus(t, "enable command", f.owner.patch(f.botPath+"/commands/"+id.String(), map[string]any{"disabled": false}), http.StatusOK)
	wantStatus(t, "disable bot", f.owner.patch(f.botPath, map[string]any{"disabled": true}), http.StatusOK)
	if got := channelCommands(t, max, f.open); len(got) != 0 {
		t.Fatalf("disabled bot listed: %+v", got)
	}
}

func TestSlashCommandRoundTrip(t *testing.T) {
	f := gwFixture(t)
	f.a.router.Bots.SetLimits(1000, 1000)
	max := f.a.register(f.owner, "max")
	wantStatus(t, "register", f.client.post(bot.BasePath+"/commands", pingCommand), http.StatusOK)
	cmd := channelCommands(t, max, f.open)[0]
	run := func(opts map[string]any) response {
		return max.post("/api/channels/"+f.open.String()+"/interactions", map[string]any{"command_id": cmd.ID, "options": opts})
	}

	// Offline bots cannot be reached.
	wantStatus(t, "bot offline", run(map[string]any{"text": "hi"}), http.StatusConflict)

	g := f.dialGW("Bot " + f.token.Secret)
	g.identify("", 0) // interactions need no intent
	maxWS := mustDial(t, max)

	// Option validation.
	for name, opts := range map[string]map[string]any{
		"missing required": {},
		"unknown option":   {"text": "x", "nope": 1},
		"wrong type":       {"text": 5},
		"fraction":         {"text": "x", "times": 1.5},
		"not a bool":       {"text": "x", "loud": "yes"},
		"not an id":        {"text": "x", "who": "max"},
		"unknown member":   {"text": "x", "who": uuid.NewString()},
		"hidden channel":   {"text": "x", "where": uuid.NewString()},
	} {
		wantStatus(t, name, run(opts), http.StatusBadRequest)
	}

	res := run(map[string]any{"text": "hello", "times": 3, "loud": true, "who": max.user.ID, "where": f.open})
	wantStatus(t, "run", res, http.StatusAccepted)
	var inv bot.Invoked
	res.decode(t, &inv)
	_, d := g.expect("INTERACTION_CREATE")
	var ev struct {
		ID        uuid.UUID `json:"id"`
		Token     string    `json:"token"`
		Type      int       `json:"type"`
		ChannelID uuid.UUID `json:"channel_id"`
		Data      struct {
			ID      uuid.UUID `json:"id"`
			Name    string    `json:"name"`
			Options []struct {
				Name  string `json:"name"`
				Type  int    `json:"type"`
				Value any    `json:"value"`
			} `json:"options"`
		} `json:"data"`
		Member struct {
			User struct {
				ID       uuid.UUID `json:"id"`
				Username string    `json:"username"`
			} `json:"user"`
		} `json:"member"`
	}
	if err := json.Unmarshal(d, &ev); err != nil {
		t.Fatal(err)
	}
	if ev.ID != inv.InteractionID || ev.Token == "" || ev.Type != 2 || ev.ChannelID != f.open || ev.Data.ID != cmd.ID || ev.Data.Name != "ping" ||
		ev.Member.User.ID != max.user.ID || ev.Member.User.Username != "max" || len(ev.Data.Options) != 5 || ev.Data.Options[0].Value != "hello" || ev.Data.Options[1].Value != float64(3) {
		t.Fatalf("interaction: %s", d)
	}

	reply := func(path string, body any) response {
		return f.client.post(bot.BasePath+"/interactions/"+ev.ID.String()+"/"+ev.Token+path, body)
	}
	// Followups need the first response; a wrong token or another bot's token fail.
	wantStatus(t, "followup first", reply("/followups", map[string]any{"content": "x"}), http.StatusConflict)
	wantStatus(t, "wrong token", f.client.post(bot.BasePath+"/interactions/"+ev.ID.String()+"/nope/callback", map[string]any{"type": 4, "data": map[string]any{"content": "x"}}), http.StatusNotFound)
	other := createBot(t, f.owner, "second")
	otherTok := issueToken(t, f.owner, other.ID, map[string]any{})
	thief := &botClient{t: t, a: f.a, token: otherTok.Secret}
	wantStatus(t, "another bot", thief.post(bot.BasePath+"/interactions/"+ev.ID.String()+"/"+ev.Token+"/callback", map[string]any{"type": 4, "data": map[string]any{"content": "x"}}), http.StatusNotFound)
	wantStatus(t, "bad type", reply("/callback", map[string]any{"type": 9}), http.StatusBadRequest)

	// The first response: a public message from the bot.
	wantStatus(t, "empty", reply("/callback", map[string]any{"type": 4, "data": map[string]any{"content": ""}}), http.StatusBadRequest)
	wantStatus(t, "respond", reply("/callback", map[string]any{"type": 4, "data": map[string]any{"content": "pong **hello**", "embeds": []map[string]any{{"title": "ping"}}}}), http.StatusNoContent)
	if p, ok := maxWS.expect("message_create", soon); !ok || !strings.Contains(string(p), "pong **hello**") || !strings.Contains(string(p), f.bot.UserID.String()) {
		t.Fatalf("no public response: %s", p)
	}
	wantStatus(t, "respond twice", reply("/callback", map[string]any{"type": 4, "data": map[string]any{"content": "again"}}), http.StatusConflict)

	// Follow-ups: public, and ephemeral (only the invoker's socket hears it, and nothing is stored).
	res = reply("/followups", map[string]any{"content": "second message"})
	wantStatus(t, "followup", res, http.StatusCreated)
	maxWS.expect("message_create", soon)
	ownerWS := mustDial(t, f.owner)
	wantStatus(t, "ephemeral", reply("/followups", map[string]any{"content": "just for you", "flags": 64}), http.StatusNoContent)
	p, ok := maxWS.expect("interaction_response", soon)
	if !ok || !strings.Contains(string(p), "just for you") || !strings.Contains(string(p), f.open.String()) {
		t.Fatalf("ephemeral: %s", p)
	}
	if p, ok := ownerWS.expect("interaction_response", none); ok {
		t.Fatalf("an ephemeral response reached someone else: %s", p)
	}
	hist := f.owner.get("/api/channels/" + f.open.String() + "/messages")
	if strings.Contains(string(hist.body), "just for you") {
		t.Fatal("an ephemeral response was stored")
	}
	// 5 responses at most (callback + 4 followups).
	for i := 0; i < 2; i++ {
		wantStatus(t, "followup", reply("/followups", map[string]any{"content": "more", "flags": 64}), http.StatusNoContent)
	}
	wantStatus(t, "sixth", reply("/followups", map[string]any{"content": "too many", "flags": 64}), http.StatusConflict)

	// Deferred responses only acknowledge.
	res = run(map[string]any{"text": "later"})
	wantStatus(t, "run again", res, http.StatusAccepted)
	_, d = g.expect("INTERACTION_CREATE")
	_ = json.Unmarshal(d, &ev)
	wantStatus(t, "deferred", reply("/callback", map[string]any{"type": 5}), http.StatusNoContent)
	wantStatus(t, "late followup", reply("/followups", map[string]any{"content": "done"}), http.StatusCreated)
	wantStatus(t, "type 4 without data", reply("/callback", map[string]any{"type": 4}), http.StatusBadRequest)
}

func TestSlashCommandPermissions(t *testing.T) {
	f := gwFixture(t)
	f.a.router.Bots.SetLimits(1000, 1000)
	wantStatus(t, "register", f.client.post(bot.BasePath+"/commands", pingCommand), http.StatusOK)
	g := f.dialGW("Bot " + f.token.Secret)
	g.identify("", 0)

	max := f.a.register(f.owner, "max")
	cmd := channelCommands(t, max, f.open)[0]
	run := func(c *client, ch uuid.UUID, id uuid.UUID) response {
		return c.post("/api/channels/"+ch.String()+"/interactions", map[string]any{"command_id": id, "options": map[string]any{"text": "x"}})
	}
	wantStatus(t, "elsewhere", run(max, f.other, cmd.ID), http.StatusNotFound)
	wantStatus(t, "unknown command", run(max, f.open, uuid.New()), http.StatusNotFound)
	wantStatus(t, "not json", max.post("/api/channels/"+f.open.String()+"/interactions", "nope"), http.StatusBadRequest)

	// A member who may not send messages cannot run commands.
	muted := createRole(t, f.owner, "Muted", 0)
	wantStatus(t, "deny send", putOverwrites(f.owner, "channels", f.open,
		overwrite("role", f.roleID, perm.ViewChannel, 0), overwrite("role", muted, 0, perm.SendMessages)), http.StatusNoContent)
	assignRoles(t, f.owner, max, muted)
	wantStatus(t, "muted member", run(max, f.open, cmd.ID), http.StatusForbidden)

	// Switched-off commands refuse a run even when the id is known.
	other := f.a.register(f.owner, "other")
	wantStatus(t, "disable", f.owner.patch(f.botPath+"/commands/"+cmd.ID.String(), map[string]any{"disabled": true}), http.StatusOK)
	wantStatus(t, "disabled", run(other, f.open, cmd.ID), http.StatusNotFound)
	wantStatus(t, "enable", f.owner.patch(f.botPath+"/commands/"+cmd.ID.String(), map[string]any{"disabled": false}), http.StatusOK)
	wantStatus(t, "allowed", run(other, f.open, cmd.ID), http.StatusAccepted)

	// Runs are limited per member.
	var limited bool
	for i := 0; i < 25; i++ {
		if run(other, f.open, cmd.ID).status == http.StatusTooManyRequests {
			limited = true
			break
		}
	}
	if !limited {
		t.Fatal("running commands is not rate limited")
	}
	_ = gateway.OpDispatch
}

// A bot that lost its permission to send cannot answer an interaction.
func TestSlashCommandResponseNeedsSendPermission(t *testing.T) {
	f := gwFixture(t)
	f.a.router.Bots.SetLimits(1000, 1000)
	wantStatus(t, "register", f.client.post(bot.BasePath+"/commands", pingCommand), http.StatusOK)
	g := f.dialGW("Bot " + f.token.Secret)
	g.identify("", 0)
	max := f.a.register(f.owner, "max")
	cmd := channelCommands(t, max, f.open)[0]
	wantStatus(t, "run", max.post("/api/channels/"+f.open.String()+"/interactions", map[string]any{"command_id": cmd.ID, "options": map[string]any{"text": "x"}}), http.StatusAccepted)
	_, d := g.expect("INTERACTION_CREATE")
	var ev struct {
		ID    uuid.UUID `json:"id"`
		Token string    `json:"token"`
	}
	_ = json.Unmarshal(d, &ev)
	wantStatus(t, "no send", f.owner.patch("/api/admin/roles/"+f.roleID.String(), map[string]any{"permissions": perm.AddReactions}), http.StatusOK)
	res := f.client.post(bot.BasePath+"/interactions/"+ev.ID.String()+"/"+ev.Token+"/callback", map[string]any{"type": 4, "data": map[string]any{"content": "x"}})
	if res.status != http.StatusNotFound && res.status != http.StatusForbidden {
		t.Fatalf("answered without permission: %d %s", res.status, res.body)
	}
}
