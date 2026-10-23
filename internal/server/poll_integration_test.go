//go:build integration

package server

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/bot"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/poll"
	"github.com/msch128/mnema-talk/internal/slash"
)

type pollFixture struct {
	a       *app
	owner   *client
	alice   *client
	bob     *client
	channel uuid.UUID
}

func newPollFixture(t *testing.T) pollFixture {
	t.Helper()
	a := newApp(t, false)
	owner := a.seedAdmin()
	return pollFixture{a: a, owner: owner, alice: a.register(owner, "alice"), bob: a.register(owner, "bob"), channel: a.createChannel(owner, "general", "text")}
}

func (f pollFixture) base() string { return "/api/channels/" + f.channel.String() }

func pollCommands(t *testing.T, c *client, channel uuid.UUID) []bot.ChannelCommand {
	t.Helper()
	res := c.get("/api/channels/" + channel.String() + "/commands")
	wantStatus(t, "list commands", res, http.StatusOK)
	var list []bot.ChannelCommand
	res.decode(t, &list)
	return list
}

func slashPoll(c *client, channel uuid.UUID, options map[string]any) response {
	return c.post("/api/channels/"+channel.String()+"/interactions", map[string]any{"command_id": slash.ID("poll"), "options": options})
}

// createPoll posts a poll through the REST route and returns its message.
func (f pollFixture) createPoll(t *testing.T, c *client, body map[string]any) chat.Message {
	t.Helper()
	res := c.post(f.base()+"/polls", body)
	wantStatus(t, "create poll", res, http.StatusCreated)
	var m chat.Message
	res.decode(t, &m)
	if m.Poll == nil {
		t.Fatalf("no poll on the message: %s", res.body)
	}
	return m
}

func (f pollFixture) basic(t *testing.T, c *client, extra map[string]any) chat.Message {
	t.Helper()
	body := map[string]any{"question": "Pizza or pasta?", "options": []string{"Pizza", "Pasta", "Both"}}
	for k, v := range extra {
		body[k] = v
	}
	return f.createPoll(t, c, body)
}

func TestPollIsInTheCommandMenuNextToBotCommands(t *testing.T) {
	f := newPollFixture(t)
	list := pollCommands(t, f.alice, f.channel)
	if len(list) == 0 || list[0].Name != "poll" || !list[0].Builtin || list[0].ID != slash.ID("poll") || list[0].BotName != "" {
		t.Fatalf("menu: %+v", list)
	}
	byName := map[string]bot.Option{}
	for _, o := range list[0].Options {
		byName[o.Name] = o
	}
	if !byName["question"].Required || !byName["answers"].Required || len(byName["duration"].Choices) != len(poll.DurationNames()) {
		t.Fatalf("options: %+v", list[0].Options)
	}

	// A member who cannot send in the channel is not offered it, and cannot run it.
	wantStatus(t, "read-only", putOverwrites(f.owner, "channels", f.channel, overwrite("role", everyoneID(t, f.owner), 0, perm.SendMessages)), http.StatusNoContent)
	for _, c := range pollCommands(t, f.alice, f.channel) {
		if c.Builtin {
			t.Fatalf("a member who cannot send is offered %s", c.Name)
		}
	}
	wantStatus(t, "run without Send messages", slashPoll(f.alice, f.channel, map[string]any{"question": "q", "answers": "a|b"}), http.StatusForbidden)
}

func TestPollCommandPostsAPollMessage(t *testing.T) {
	f := newPollFixture(t)
	res := slashPoll(f.alice, f.channel, map[string]any{
		"question": "Where do we meet?", "answers": "Park | Cafe | Home", "duration": "4h", "multiple": true, "anonymous": true,
	})
	wantStatus(t, "run /poll", res, http.StatusOK)
	var out slash.Result
	res.decode(t, &out)
	if out.MessageID == nil {
		t.Fatalf("no message in %s", res.body)
	}

	history := f.bob.get(f.base() + "/messages")
	var msgs []chat.Message
	history.decode(t, &msgs)
	if len(msgs) != 1 || msgs[0].ID != *out.MessageID || msgs[0].Content != "Where do we meet?" || msgs[0].UserID != f.alice.user.ID {
		t.Fatalf("history: %s", history.body)
	}
	p := msgs[0].Poll
	if p == nil || !p.Multiple || !p.Anonymous || p.Closed || len(p.Options) != 3 || p.Options[0].Label != "Park" || p.Options[2].Label != "Home" {
		t.Fatalf("poll: %+v", p)
	}
	if hours := p.ClosesAt.Sub(msgs[0].CreatedAt).Hours(); hours < 3.9 || hours > 4.1 {
		t.Fatalf("closes after %v hours", hours)
	}
	var announced bool
	for _, e := range f.a.events.Snapshot() {
		if m, ok := e.Payload.(*chat.Message); ok && e.Type == events.MessageCreate && m.ID == *out.MessageID && m.Poll != nil {
			announced = true
		}
	}
	if !announced {
		t.Fatal("message_create with the poll was not broadcast")
	}
}

func TestPollCommandRefusesBadInput(t *testing.T) {
	f := newPollFixture(t)
	long := strings.Repeat("x", 56)
	for name, opts := range map[string]map[string]any{
		"missing question":    {"answers": "a|b"},
		"missing answers":     {"question": "q"},
		"one answer":          {"question": "q", "answers": "a"},
		"empty pieces":        {"question": "q", "answers": "a||"},
		"eleven answers":      {"question": "q", "answers": "1|2|3|4|5|6|7|8|9|10|11"},
		"duplicate answers":   {"question": "q", "answers": "Yes|yes"},
		"long answer":         {"question": "q", "answers": "a|" + long},
		"long question":       {"question": strings.Repeat("q", 301), "answers": "a|b"},
		"duration not listed": {"question": "q", "answers": "a|b", "duration": "2h"},
		"unknown option":      {"question": "q", "answers": "a|b", "bogus": true},
		"multiple not bool":   {"question": "q", "answers": "a|b", "multiple": "yes"},
	} {
		wantStatus(t, name, slashPoll(f.alice, f.channel, opts), http.StatusBadRequest)
	}
	var msgs []chat.Message
	f.alice.get(f.base()+"/messages").decode(t, &msgs)
	if len(msgs) != 0 {
		t.Fatalf("a refused poll left %d messages", len(msgs))
	}
}

func TestPollRestRouteChecksPermissionsAndInput(t *testing.T) {
	f := newPollFixture(t)
	m := f.basic(t, f.alice, map[string]any{"duration": "1w"})
	if m.Poll.Multiple || m.Poll.Anonymous || len(m.Poll.Options) != 3 {
		t.Fatalf("poll: %+v", m.Poll)
	}
	wantStatus(t, "one option", f.alice.post(f.base()+"/polls", map[string]any{"question": "q", "options": []string{"a"}}), http.StatusBadRequest)
	wantStatus(t, "unknown field", f.alice.post(f.base()+"/polls", map[string]any{"question": "q", "options": []string{"a", "b"}, "bogus": 1}), http.StatusBadRequest)
	wantStatus(t, "no session", f.a.anon().post(f.base()+"/polls", map[string]any{"question": "q", "options": []string{"a", "b"}}), http.StatusUnauthorized)

	secret := f.a.createChannel(f.owner, "secret", "text")
	wantStatus(t, "hide", putOverwrites(f.owner, "channels", secret, overwrite("role", everyoneID(t, f.owner), 0, perm.ViewChannel)), http.StatusNoContent)
	wantStatus(t, "hidden channel", f.alice.post("/api/channels/"+secret.String()+"/polls", map[string]any{"question": "q", "options": []string{"a", "b"}}), http.StatusNotFound)
	wantStatus(t, "slash in hidden channel", slashPoll(f.alice, secret, map[string]any{"question": "q", "answers": "a|b"}), http.StatusNotFound)
}

func TestPollMessageCannotBeEditedButCanBeDeleted(t *testing.T) {
	f := newPollFixture(t)
	m := f.basic(t, f.alice, nil)
	msg := f.base() + "/messages/" + m.ID.String()
	wantStatus(t, "edit", f.alice.put(msg, map[string]any{"content": "Pizza or pasta or rice?"}), http.StatusBadRequest)
	wantStatus(t, "delete", f.alice.delete(msg), http.StatusNoContent)
	wantStatus(t, "poll is gone", f.alice.get(msg+"/poll"), http.StatusNotFound)
	var rows int
	if err := f.a.db.QueryRow(t.Context(), `SELECT (SELECT COUNT(*) FROM polls) + (SELECT COUNT(*) FROM poll_options) + (SELECT COUNT(*) FROM poll_votes)`).Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("poll rows left after delete: %d, %v", rows, err)
	}
}

func TestPollQuestionIsSearchable(t *testing.T) {
	f := newPollFixture(t)
	f.basic(t, f.alice, map[string]any{"question": "Which pineapple topping?"})
	got, status := f.bob.search(t, map[string]string{"q": "pineapple"})
	if status != http.StatusOK || len(got.Messages) != 1 {
		t.Fatalf("search: %d %+v", status, got)
	}
}
