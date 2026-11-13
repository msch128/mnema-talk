//go:build integration

package server

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/bot"
	"github.com/msch128/mnema-talk/internal/perm"
)

// A webhook pointed at a conversation's call chat past the API queues
// nothing for what is said there: the queue row would be written in the
// message's own transaction, so the absence is certain once the post
// answered, without waiting for a delivery that may come late.
func TestAWebhookOfACallChatQueuesNothing(t *testing.T) {
	f := newCallFixtureWith(t, allowLoopback)
	f.a.router.Webhooks.Outgoing.SetBackoff([]time.Duration{5 * time.Millisecond})
	talk := f.start(t)
	open := f.a.createChannel(f.admin, "alerts", "text")
	rc := newReceiver(t, nil)
	hook := makeOutgoing(t, f.admin, open, rc.srv.URL+"/hook", "MESSAGE_CREATE")
	ctx := context.Background()
	if _, err := f.a.db.Exec(ctx, `UPDATE outgoing_webhooks SET channel_id = $1 WHERE id = $2`, talk, hook.ID); err != nil {
		t.Fatal(err)
	}
	// Record and Wants must both refuse the private channel, whatever the
	// cache says: once before the reload (stale routes), once after it.
	f.a.send(f.dmFixture.alice, talk, "private words")
	if err := f.a.router.Webhooks.Outgoing.Reload(ctx); err != nil {
		t.Fatal(err)
	}
	f.a.send(f.dmFixture.bob, talk, "more private words")
	if n := f.a.countRows(`SELECT COUNT(*) FROM outgoing_queue WHERE webhook_id = $1`, hook.ID); n != 0 {
		t.Fatalf("%d queue rows for a webhook of a call chat", n)
	}
}

// A bot with server-wide View channels and Send messages still has no part
// in a conversation's call chat: its commands are not offered there and
// cannot be run there, so no interaction (nor a deferral) comes from it.
func TestABotsCommandsDoNotReachACallChat(t *testing.T) {
	f := newCallFixture(t)
	talk := f.start(t)
	role := createRole(t, f.admin, "Everywhere bot", perm.ViewChannel|perm.SendMessages)
	b := createBot(t, f.admin, "everywhere")
	wantStatus(t, "bot role", f.admin.put("/api/admin/bots/"+b.ID.String()+"/roles", map[string]any{"role_ids": []uuid.UUID{role}}), http.StatusOK)
	tok := issueToken(t, f.admin, b.ID, map[string]any{"name": "t"})
	bc := &botClient{t: t, a: f.a, token: tok.Secret, user: b.UserID}
	res := bc.post(bot.BasePath+"/commands", map[string]any{"name": "ping", "description": "Ping"})
	wantStatus(t, "register", res, http.StatusOK)
	var cmd bot.Command
	res.decode(t, &cmd)

	res = f.dmFixture.alice.get("/api/channels/" + talk.String() + "/commands")
	wantStatus(t, "commands of the call chat", res, http.StatusOK)
	var listed []bot.ChannelCommand
	res.decode(t, &listed)
	for _, c := range listed {
		if c.ID == cmd.ID {
			t.Fatal("a bot's command is offered in a call chat")
		}
	}
	wantStatus(t, "run in the call chat", f.dmFixture.alice.post("/api/channels/"+talk.String()+"/interactions",
		map[string]any{"command_id": cmd.ID, "options": map[string]any{}}), http.StatusNotFound)
	wantStatus(t, "post in the call chat", bc.say(talk, "hello"), http.StatusNotFound)
}
