//go:build integration

package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/bot"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/perm"
)

func componentIDs(rows []core.ActionRow) []string {
	var ids []string
	for _, r := range rows {
		for _, c := range r.Components {
			ids = append(ids, c.CustomID)
		}
	}
	return ids
}

func messageOf(t *testing.T, raw json.RawMessage) core.Message {
	t.Helper()
	var m core.Message
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// A bot's components are stored with its message: the answer, the gateway's
// MESSAGE_CREATE, the members' message_create and every later read carry them.
func TestABotMessageCarriesItsComponentsEverywhere(t *testing.T) {
	f := gwFixture(t)
	max := f.a.register(f.owner, "max")
	maxWS := mustDial(t, max)
	g := f.dialGW("Bot " + f.token.Secret)
	g.identify("", bot.IntentGuildMessages)

	m := f.postWithComponents(t, f.open, "Deploy v2.4 to production?", deployRows())
	if got := componentIDs(m.Components); len(m.Components) != 2 || strings.Join(got, ",") != "deploy:v2.4,cancel,,off,env" {
		t.Fatalf("answer: %v", got)
	}
	if link := m.Components[0].Components[2]; link.URL != "https://example.org/changes" || link.Style != core.ButtonLink {
		t.Fatalf("link button: %+v", link)
	}
	_, d := g.expect("MESSAGE_CREATE")
	if got := messageOf(t, d); len(got.Components) != 2 || got.Components[1].Components[0].Options[1].Value != "prod" {
		t.Fatalf("MESSAGE_CREATE: %s", d)
	}
	p, ok := maxWS.expect("message_create", soon)
	if !ok || len(messageOf(t, p).Components) != 2 {
		t.Fatalf("message_create: %s", p)
	}
	res := f.client.get(botPath(f.open, "/messages/"+m.ID.String()))
	wantStatus(t, "bot read", res, http.StatusOK)
	if got := messageOf(t, res.body); len(got.Components) != 2 {
		t.Fatalf("bot read: %s", res.body)
	}
	var list []core.Message
	max.get("/api/channels/"+f.open.String()+"/messages").decode(t, &list)
	if len(list) != 1 || len(list[0].Components) != 2 {
		t.Fatalf("member history: %+v", list)
	}

	// A message may be its components alone; nothing at all is still refused.
	only := f.postWithComponents(t, f.open, "", []map[string]any{selectRow(5, "who", nil)})
	if only.Content != "" || len(only.Components) != 1 {
		t.Fatalf("components only: %+v", only)
	}
	res = f.client.post(botPath(f.open, "/messages"), map[string]any{"content": "", "components": []any{}})
	wantStatus(t, "nothing", res, http.StatusBadRequest)
	if e := decodeDiscord(t, res); e.Code != 50006 {
		t.Fatalf("nothing: %s", res.body)
	}
}

func TestEditingABotMessageChangesItsComponentsAndKeepsTheRest(t *testing.T) {
	f := gwFixture(t)
	max := f.a.register(f.owner, "max")
	maxWS := mustDial(t, max)
	g := f.dialGW("Bot " + f.token.Secret)
	g.identify("", bot.IntentGuildMessages)
	m := f.postWithComponents(t, f.open, "Pick one", deployRows())
	g.expect("MESSAGE_CREATE")
	maxWS.expect("message_create", soon)
	path := botPath(f.open, "/messages/"+m.ID.String())

	res := f.client.patch(path, map[string]any{"components": []map[string]any{{"type": 1, "components": []map[string]any{
		{"type": 2, "style": 1, "label": "Again", "custom_id": "again"}}}}})
	wantStatus(t, "components only", res, http.StatusOK)
	edited := messageOf(t, res.body)
	if edited.Content != "Pick one" || edited.IsEdited || strings.Join(componentIDs(edited.Components), ",") != "again" {
		t.Fatalf("components only: %s", res.body)
	}
	if _, d := g.expect("MESSAGE_UPDATE"); strings.Join(componentIDs(messageOf(t, d).Components), ",") != "again" {
		t.Fatalf("MESSAGE_UPDATE: %s", d)
	}
	if p, ok := maxWS.expect("message_update", soon); !ok || len(messageOf(t, p).Components) != 1 {
		t.Fatalf("message_update: %s", p)
	}

	// A content-only edit keeps the components and marks the message edited.
	res = f.client.patch(path, map[string]any{"content": "Pick again"})
	wantStatus(t, "content only", res, http.StatusOK)
	if got := messageOf(t, res.body); got.Content != "Pick again" || !got.IsEdited || len(got.Components) != 1 {
		t.Fatalf("content only: %s", res.body)
	}
	// The text of a message with components may be emptied, but not both.
	wantStatus(t, "empty text", f.client.patch(path, map[string]any{"content": ""}), http.StatusOK)
	wantStatus(t, "nothing left", f.client.patch(path, map[string]any{"components": []any{}}), http.StatusBadRequest)
	res = f.client.patch(path, map[string]any{"content": "back", "components": []any{}})
	wantStatus(t, "remove", res, http.StatusOK)
	if got := messageOf(t, res.body); got.Content != "back" || got.Components != nil {
		t.Fatalf("remove: %s", res.body)
	}
	var stored int
	_ = f.a.db.QueryRow(t.Context(), `SELECT COUNT(*) FROM message_components WHERE message_id = $1`, m.ID).Scan(&stored)
	if stored != 0 {
		t.Fatal("removed components are still stored")
	}
	// A plain text message still cannot be emptied, with the same answer as before.
	res = f.client.patch(path, map[string]any{"content": " "})
	wantStatus(t, "empty plain", res, http.StatusBadRequest)
	if e := decodeDiscord(t, res); e.Message != "content is required" || e.Code != 50035 {
		t.Fatalf("empty plain: %s", res.body)
	}
	wantStatus(t, "unknown field", f.client.patch(path, map[string]any{"content": "x", "flags": 1}), http.StatusBadRequest)

	// Members never set components: their routes refuse the field.
	own := postMessage(t, max, f.open, map[string]any{"content": "mine"})
	wantStatus(t, "member post", max.post("/api/channels/"+f.open.String()+"/messages", map[string]any{"content": "x", "components": deployRows()}), http.StatusBadRequest)
	wantStatus(t, "member edit", max.put("/api/channels/"+f.open.String()+"/messages/"+own.String(), map[string]any{"content": "x", "components": deployRows()}), http.StatusBadRequest)
}

func TestComponentsThatBreakTheRulesAreRefusedAsAnInvalidForm(t *testing.T) {
	cf := newComponentsFixture(t)
	button := func(id string) map[string]any {
		return map[string]any{"type": 2, "style": 1, "label": "x", "custom_id": id}
	}
	six := make([]map[string]any, 6)
	for i := range six {
		six[i] = map[string]any{"type": 1, "components": []map[string]any{button(string(rune('a' + i)))}}
	}
	bad := map[string]any{
		"a mentionable select": []map[string]any{selectRow(7, "m", nil)},
		"a duplicate custom_id": []map[string]any{{"type": 1, "components": []map[string]any{button("same")}},
			{"type": 1, "components": []map[string]any{button("same")}}},
		"six rows": six,
	}
	m := cf.postWithComponents(t, cf.open, "ok", deployRows())
	in := cf.command(t)
	for name, rows := range bad {
		for what, res := range map[string]response{
			"post":     cf.client.post(botPath(cf.open, "/messages"), map[string]any{"content": "x", "components": rows}),
			"edit":     cf.client.patch(botPath(cf.open, "/messages/"+m.ID.String()), map[string]any{"components": rows}),
			"callback": cf.callback(in, 4, map[string]any{"content": "x", "components": rows}),
		} {
			wantStatus(t, name+" "+what, res, http.StatusBadRequest)
			if e := decodeDiscord(t, res); e.Code != 50035 {
				t.Errorf("%s %s: %s", name, what, res.body)
			}
		}
	}
	// A bot that may not post here is told that first.
	res := cf.client.post(botPath(cf.other, "/messages"), map[string]any{"content": "x", "components": bad["six rows"]})
	if res.status != http.StatusForbidden && res.status != http.StatusNotFound {
		t.Fatalf("refusal first: %d %s", res.status, res.body)
	}
	if n := cf.a.count(`SELECT COUNT(*) FROM messages WHERE channel_id = $1`, cf.open); n != 1 {
		t.Fatalf("a refused message was stored: %d", n)
	}
}

func TestAClickReachesTheBotWithTheMessageAndTheMember(t *testing.T) {
	cf := newComponentsFixture(t)
	m := cf.postWithComponents(t, cf.open, "Deploy?", deployRows())
	res := click(cf.max, cf.open, m.ID, "deploy:v2.4")
	wantStatus(t, "click", res, http.StatusAccepted)
	var inv bot.Invoked
	res.decode(t, &inv)
	in := cf.interaction(t)
	if in.ID != inv.InteractionID || in.Type != bot.InteractionMessageComponent || in.ChannelID != cf.open || in.AppID != cf.bot.ID {
		t.Fatalf("interaction: %s", in.raw)
	}
	if in.Data.CustomID != "deploy:v2.4" || in.Data.ComponentType != core.ComponentButton || in.Data.Values != nil || in.Member.User.ID != cf.max.user.ID {
		t.Fatalf("data: %s", in.raw)
	}
	if got := messageOf(t, in.Message); got.ID != m.ID || got.Content != "Deploy?" || len(got.Components) != 2 {
		t.Fatalf("message: %s", in.Message)
	}

	for name, res := range map[string]response{
		"unknown custom_id":  click(cf.max, cf.open, m.ID, "nope"),
		"disabled button":    click(cf.max, cf.open, m.ID, "off"),
		"link button":        click(cf.max, cf.open, m.ID, ""),
		"unknown message":    click(cf.max, cf.open, uuid.New(), "cancel"),
		"another channel":    click(cf.max, cf.other, m.ID, "cancel"),
		"a member's message": click(cf.max, cf.open, postMessage(t, cf.max, cf.open, map[string]any{"content": "hi"}), "cancel"),
	} {
		wantStatus(t, name, res, http.StatusNotFound)
	}
	wantStatus(t, "values on a button", click(cf.max, cf.open, m.ID, "cancel", "x"), http.StatusBadRequest)
	wantStatus(t, "bad body", cf.max.post(clickPath(cf.open, m.ID), map[string]any{"custom_id": "cancel", "extra": 1}), http.StatusBadRequest)
	cf.g.nothing()
}

func TestAComponentIsUsableOnlyWhileItsBotAndTheMemberMayUseIt(t *testing.T) {
	cf := newComponentsFixture(t)
	m := cf.postWithComponents(t, cf.open, "Deploy?", deployRows())

	// A channel the member cannot see answers like a missing one.
	secret := cf.a.createChannel(cf.owner, "staff-only", "text")
	wantStatus(t, "private", putOverwrites(cf.owner, "channels", secret,
		overwrite("role", everyoneID(t, cf.owner), 0, perm.ViewChannel), overwrite("role", cf.roleID, perm.ViewChannel, 0)), http.StatusNoContent)
	hidden := cf.postWithComponents(t, secret, "staff", deployRows())
	wantStatus(t, "hidden channel", click(cf.max, secret, hidden.ID, "cancel"), http.StatusNotFound)
	wantStatus(t, "the Owner sees it", click(cf.owner, secret, hidden.ID, "cancel"), http.StatusAccepted)
	cf.interaction(t)

	// Timed out: still reads, cannot click.
	wantStatus(t, "timeout", cf.owner.post("/api/admin/users/"+cf.max.user.ID.String()+"/timeout", map[string]any{"duration_seconds": 600}), http.StatusOK)
	wantStatus(t, "timed out", click(cf.max, cf.open, m.ID, "cancel"), http.StatusForbidden)
	wantStatus(t, "lift", cf.owner.delete("/api/admin/users/"+cf.max.user.ID.String()+"/timeout"), http.StatusNoContent)
	wantStatus(t, "lifted", click(cf.max, cf.open, m.ID, "cancel"), http.StatusAccepted)
	cf.interaction(t)

	// Rules first.
	setRules(t, cf.owner, "# Rules\nBe kind.", false)
	res := click(cf.max, cf.open, m.ID, "cancel")
	if res.status != http.StatusForbidden || res.code(t) != "RULES_NOT_ACCEPTED" {
		t.Fatalf("rules: %d %s", res.status, res.body)
	}
	acceptRules(t, cf.max)

	// A read-only channel: clicks still work.
	wantStatus(t, "read only", putOverwrites(cf.owner, "channels", cf.open,
		overwrite("role", everyoneID(t, cf.owner), 0, perm.SendMessages), overwrite("role", cf.roleID, perm.ViewChannel, 0)), http.StatusNoContent)
	wantStatus(t, "read-only click", click(cf.max, cf.open, m.ID, "cancel"), http.StatusAccepted)
	cf.interaction(t)

	// The bot losing the channel, being disabled or offline.
	wantStatus(t, "bot loses view", putOverwrites(cf.owner, "channels", cf.open), http.StatusNoContent)
	wantStatus(t, "bot cannot view", click(cf.max, cf.open, m.ID, "cancel"), http.StatusNotFound)
	wantStatus(t, "bot view back", putOverwrites(cf.owner, "channels", cf.open, overwrite("role", cf.roleID, perm.ViewChannel, 0)), http.StatusNoContent)
	cf.goOffline(t)
	wantStatus(t, "offline", click(cf.max, cf.open, m.ID, "cancel"), http.StatusConflict)
	wantStatus(t, "disable", cf.owner.patch(cf.botPath, map[string]any{"disabled": true}), http.StatusOK)
	wantStatus(t, "disabled bot", click(cf.max, cf.open, m.ID, "cancel"), http.StatusNotFound)
}

func TestClicksAndModalSubmitsShareALimitPerMember(t *testing.T) {
	cf := newComponentsFixture(t)
	m := cf.postWithComponents(t, cf.open, "Deploy?", deployRows())
	limited := 0
	for i := 0; i < 31; i++ {
		res := click(cf.max, cf.open, m.ID, "cancel")
		if res.status == http.StatusTooManyRequests {
			if res.header.Get("Retry-After") == "" {
				t.Fatal("429 without Retry-After")
			}
			limited = i
			break
		}
		wantStatus(t, "click", res, http.StatusAccepted)
	}
	if limited != 30 {
		t.Fatalf("limited after %d clicks, want 30", limited)
	}
	wantStatus(t, "modal submit counts too", cf.max.post(submitPath(cf.open, uuid.New()), map[string]any{"values": map[string]string{}}), http.StatusTooManyRequests)
	wantStatus(t, "another member", click(cf.owner, cf.open, m.ID, "cancel"), http.StatusAccepted)
}

func TestSelectValuesMustFitTheirSelect(t *testing.T) {
	cf := newComponentsFixture(t)
	staff := createRole(t, cf.owner, "Staff", 0)
	var botRole uuid.UUID
	var roles []perm.Role
	cf.owner.get("/api/roles").decode(t, &roles)
	for _, r := range roles {
		if r.Managed {
			botRole = r.ID
		}
	}
	rows := []map[string]any{
		deployRows()[1],
		selectRow(5, "who", map[string]any{"max_values": 2}),
		selectRow(6, "role", nil),
		selectRow(8, "where", map[string]any{"min_values": 0}),
	}
	m := cf.postWithComponents(t, cf.open, "Choose", rows)
	use := func(id string, values ...string) response { return click(cf.max, cf.open, m.ID, id, values...) }

	wantStatus(t, "two options", use("env", "staging", "prod"), http.StatusAccepted)
	if in := cf.interaction(t); strings.Join(in.Data.Values, ",") != "staging,prod" || in.Data.ComponentType != 3 {
		t.Fatalf("values: %s", in.raw)
	}
	wantStatus(t, "a member", use("who", cf.owner.user.ID.String()), http.StatusAccepted)
	in := cf.interaction(t)
	if u := in.Data.Resolved.Users[cf.owner.user.ID.String()]; u["username"] != cf.owner.user.Username {
		t.Fatalf("resolved users: %s", in.raw)
	}
	wantStatus(t, "a role", use("role", staff.String()), http.StatusAccepted)
	if in := cf.interaction(t); in.Data.Resolved.Roles[staff.String()]["name"] != "Staff" {
		t.Fatalf("resolved roles: %s", in.raw)
	}
	wantStatus(t, "a channel", use("where", cf.open.String()), http.StatusAccepted)
	if in := cf.interaction(t); in.Data.Resolved.Channels[cf.open.String()]["name"] != "open" {
		t.Fatalf("resolved channels: %s", in.raw)
	}
	wantStatus(t, "no channel", use("where"), http.StatusAccepted)
	cf.interaction(t)

	for name, res := range map[string]response{
		"not an option":            use("env", "dev"),
		"an option twice":          use("env", "prod", "prod"),
		"too many":                 use("env", "a", "b", "c"),
		"too few":                  use("env"),
		"not an id":                use("who", "max"),
		"a member who is not":      use("who", uuid.NewString()),
		"a bot":                    use("who", cf.bot.UserID.String()),
		"@everyone":                use("role", everyoneID(t, cf.owner).String()),
		"a managed role":           use("role", botRole.String()),
		"a role that is not":       use("role", uuid.NewString()),
		"a channel the bot cannot": use("where", cf.other.String()),
		"a channel that is not":    use("where", uuid.NewString()),
	} {
		wantStatus(t, name, res, http.StatusBadRequest)
	}
	cf.g.nothing()
}
