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

func (f *botFixture) setSlowMode(t *testing.T, seconds int) {
	t.Helper()
	res := f.owner.patch("/api/admin/channels/"+f.open.String(), map[string]any{"slow_mode_seconds": seconds})
	wantStatus(t, "slow mode", res, http.StatusOK)
}

// allowManageMessages adds Manage messages to the bot's role.
func (f *botFixture) allowManageMessages(t *testing.T) {
	t.Helper()
	perms := perm.SendMessages | perm.AddReactions | perm.AttachFiles | perm.ManageMessages
	wantStatus(t, "manage messages", f.owner.patch("/api/admin/roles/"+f.roleID.String(), map[string]any{"permissions": perms}), http.StatusOK)
}

func TestSlowModeHoldsABotBackUnlessItMayManageMessages(t *testing.T) {
	f := newBotFixture(t, false)
	f.setSlowMode(t, 600)
	wantStatus(t, "first message", f.client.say(f.open, "one"), http.StatusCreated)
	wantStatus(t, "second message in the wait", f.client.say(f.open, "two"), http.StatusTooManyRequests)

	f.allowManageMessages(t)
	wantStatus(t, "with Manage messages", f.client.say(f.open, "three"), http.StatusCreated)
	wantStatus(t, "and again", f.client.say(f.open, "four"), http.StatusCreated)
}

func TestSlowModeAppliesToACommandResponseLikeToTheBotsMessages(t *testing.T) {
	f := gwFixture(t)
	f.a.router.Bots.SetLimits(1000, 1000)
	wantStatus(t, "register", f.client.post(bot.BasePath+"/commands", pingCommand), http.StatusOK)
	g := f.dialGW("Bot " + f.token.Secret)
	g.identify("", 0)
	max := f.a.register(f.owner, "max")
	cmd := channelCommands(t, max, f.open)[0]
	respond := func() response {
		t.Helper()
		run := max.post("/api/channels/"+f.open.String()+"/interactions", map[string]any{"command_id": cmd.ID, "options": map[string]any{"text": "x"}})
		wantStatus(t, "run", run, http.StatusAccepted)
		_, d := g.expect("INTERACTION_CREATE")
		var ev struct {
			ID    uuid.UUID `json:"id"`
			Token string    `json:"token"`
		}
		if err := json.Unmarshal(d, &ev); err != nil {
			t.Fatal(err)
		}
		return f.client.post(bot.BasePath+"/interactions/"+ev.ID.String()+"/"+ev.Token+"/callback", map[string]any{"type": 4, "data": map[string]any{"content": "pong"}})
	}

	f.setSlowMode(t, 600)
	wantStatus(t, "first response", respond(), http.StatusNoContent)
	wantStatus(t, "second response in the wait", respond(), http.StatusTooManyRequests)

	f.allowManageMessages(t)
	wantStatus(t, "with Manage messages", respond(), http.StatusNoContent)
}
