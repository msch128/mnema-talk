//go:build integration

package server

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/bot"
	"github.com/msch128/mnema-talk/internal/perm"
)

// The events of a bot's components, ephemeral notes and modals, as the
// members' sessions get them.
func TestWireFormatOfBotComponentEvents(t *testing.T) {
	w := newWireWorld(t, "event_wire_bots")
	f := w.botWithGateway()
	m := w.id(f.client.post(botPath(w.ch, "/messages"), map[string]any{"content": "Deploy?", "components": deployRows()}))
	w.a.events.Take()
	w.step("bot message with components", f.client.post(botPath(w.ch, "/messages"), map[string]any{"content": "", "components": againRow()}))
	w.step("bot edits the components", f.client.patch(botPath(w.ch, "/messages/"+m.String()), map[string]any{"components": againRow()}))

	run := func() botInteraction {
		res := f.client.post(bot.BasePath+"/commands", map[string]any{"name": "ping", "description": "Ping"})
		var cmd bot.Command
		w.quiet(res).decode(t, &cmd)
		w.quiet(w.alice.post("/api/channels/"+w.ch.String()+"/interactions", map[string]any{"command_id": cmd.ID, "options": map[string]any{}}))
		_, d := f.gw.expect("INTERACTION_CREATE")
		var in botInteraction
		if err := json.Unmarshal(d, &in); err != nil {
			t.Fatal(err)
		}
		return in
	}
	answer := func(in botInteraction, method, rest string, body any) response {
		return f.client.req(method, bot.BasePath+"/interactions/"+in.ID.String()+"/"+in.Token+rest, body, nil)
	}
	in := run()
	w.step("ephemeral response with components", answer(in, http.MethodPost, "/callback", map[string]any{"type": 4,
		"data": map[string]any{"content": "Only you", "flags": 64, "components": againRow()}}))
	w.step("ephemeral response replaced", answer(in, http.MethodPatch, "/messages/@original", map[string]any{"content": "Still only you"}))
	w.step("ephemeral response deleted", answer(in, http.MethodDelete, "/messages/@original", nil))
	in = run()
	w.step("modal opened", answer(in, http.MethodPost, "/callback", map[string]any{"type": 9, "data": feedbackModal()}))
}

// wireBot is a bot of the wire world, connected to the gateway.
type wireBot struct {
	client *botClient
	gw     *gwClient
}

// botWithGateway adds a bot that may view and send in the world's channel and
// connects it to the gateway; nothing of it is recorded.
func (w *wireWorld) botWithGateway() wireBot {
	w.t.Helper()
	role := createRole(w.t, w.admin, "Helper bot", perm.SendMessages)
	b := createBot(w.t, w.admin, "helper")
	w.quiet(w.admin.put("/api/admin/bots/"+b.ID.String()+"/roles", map[string]any{"role_ids": []uuid.UUID{role}}))
	w.quiet(putOverwrites(w.admin, "channels", w.ch, overwrite("role", role, perm.ViewChannel, 0)))
	tok := issueToken(w.t, w.admin, b.ID, map[string]any{"name": "wire"})
	f := &botFixture{a: w.a, owner: w.admin, bot: b, token: tok, client: &botClient{t: w.t, a: w.a, token: tok.Secret, user: b.UserID}}
	g := f.dialGW("Bot " + tok.Secret)
	g.identify("", 0)
	w.a.events.Take()
	return wireBot{client: f.client, gw: g}
}
