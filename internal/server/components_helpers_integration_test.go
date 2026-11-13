//go:build integration

package server

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/bot"
	"github.com/msch128/mnema-talk/internal/chat/core"
)

// componentsFixture is a bot connected to the gateway without intents (only
// interactions reach it), a member max who can view the bot's channel and
// max's WebSocket.
type componentsFixture struct {
	*botFixture
	g     *gwClient
	max   *client
	maxWS *wsConn
}

func newComponentsFixture(t *testing.T) *componentsFixture {
	t.Helper()
	f := gwFixture(t)
	f.a.router.Bots.SetLimits(1000, 1000)
	cf := &componentsFixture{botFixture: f, max: f.a.register(f.owner, "max")}
	cf.g = f.dialGW("Bot " + f.token.Secret)
	cf.g.identify("", 0)
	cf.maxWS = mustDial(t, cf.max)
	return cf
}

// deployRows is the example of doc/bots.md: three buttons and a string select.
func deployRows() []map[string]any {
	return []map[string]any{
		{"type": 1, "components": []map[string]any{
			{"type": 2, "style": 3, "label": "Deploy", "custom_id": "deploy:v2.4"},
			{"type": 2, "style": 2, "label": "Cancel", "custom_id": "cancel"},
			{"type": 2, "style": 5, "label": "Changelog", "url": "https://example.org/changes"},
			{"type": 2, "style": 4, "label": "Off", "custom_id": "off", "disabled": true},
		}},
		{"type": 1, "components": []map[string]any{
			{"type": 3, "custom_id": "env", "placeholder": "Environment", "max_values": 2, "options": []map[string]any{
				{"label": "Staging", "value": "staging"}, {"label": "Production", "value": "prod", "default": true}}},
		}},
	}
}

// selectRow is one action row holding a select of typ with custom_id id.
func selectRow(typ int, id string, extra map[string]any) map[string]any {
	c := map[string]any{"type": typ, "custom_id": id}
	for k, v := range extra {
		c[k] = v
	}
	return map[string]any{"type": 1, "components": []map[string]any{c}}
}

// postWithComponents posts a bot message with components in ch and returns it.
func (f *botFixture) postWithComponents(t *testing.T, ch uuid.UUID, content string, rows any) core.Message {
	t.Helper()
	res := f.client.post(botPath(ch, "/messages"), map[string]any{"content": content, "components": rows})
	wantStatus(t, "post with components", res, http.StatusCreated)
	var m core.Message
	res.decode(t, &m)
	return m
}

func clickPath(ch, msg uuid.UUID) string {
	return "/api/channels/" + ch.String() + "/messages/" + msg.String() + "/components"
}

func click(c *client, ch, msg uuid.UUID, customID string, values ...string) response {
	body := map[string]any{"custom_id": customID}
	if values != nil {
		body["values"] = values
	}
	return c.post(clickPath(ch, msg), body)
}

// botInteraction is an INTERACTION_CREATE as the bot reads it.
type botInteraction struct {
	ID        uuid.UUID       `json:"id"`
	Token     string          `json:"token"`
	Type      int             `json:"type"`
	ChannelID uuid.UUID       `json:"channel_id"`
	AppID     uuid.UUID       `json:"application_id"`
	Message   json.RawMessage `json:"message"`
	Data      struct {
		ID            uuid.UUID `json:"id"`
		Name          string    `json:"name"`
		CustomID      string    `json:"custom_id"`
		ComponentType int       `json:"component_type"`
		Values        []string  `json:"values"`
		Resolved      struct {
			Users    map[string]map[string]any `json:"users"`
			Roles    map[string]map[string]any `json:"roles"`
			Channels map[string]map[string]any `json:"channels"`
		} `json:"resolved"`
		Components []struct {
			Type       int `json:"type"`
			Components []struct {
				Type     int    `json:"type"`
				CustomID string `json:"custom_id"`
				Value    string `json:"value"`
			} `json:"components"`
		} `json:"components"`
	} `json:"data"`
	Member struct {
		User struct {
			ID uuid.UUID `json:"id"`
		} `json:"user"`
	} `json:"member"`
	raw json.RawMessage
}

func (cf *componentsFixture) interaction(t *testing.T) botInteraction {
	t.Helper()
	_, d := cf.g.expect("INTERACTION_CREATE")
	var in botInteraction
	if err := json.Unmarshal(d, &in); err != nil {
		t.Fatal(err)
	}
	in.raw = d
	return in
}

// answer posts to an interaction's route below /interactions/{id}/{token}.
func (cf *componentsFixture) answer(in botInteraction, method, rest string, body any) response {
	return cf.client.req(method, bot.BasePath+"/interactions/"+in.ID.String()+"/"+in.Token+rest, body, nil)
}

func (cf *componentsFixture) callback(in botInteraction, typ int, data any) response {
	body := map[string]any{"type": typ}
	if data != nil {
		body["data"] = data
	}
	return cf.answer(in, http.MethodPost, "/callback", body)
}

// command registers /ping and runs it as max; the bot reads the interaction.
func (cf *componentsFixture) command(t *testing.T) botInteraction {
	t.Helper()
	wantStatus(t, "run", cf.run(t), http.StatusAccepted)
	return cf.interaction(t)
}

func (cf *componentsFixture) run(t *testing.T) response {
	t.Helper()
	res := cf.client.post(bot.BasePath+"/commands", map[string]any{"name": "ping", "description": "Ping"})
	wantStatus(t, "register", res, http.StatusOK)
	var cmd bot.Command
	res.decode(t, &cmd)
	return cf.max.post("/api/channels/"+cf.open.String()+"/interactions", map[string]any{"command_id": cmd.ID, "options": map[string]any{}})
}

// goOffline closes the bot's connection and waits until the server knows:
// a command run answers 409.
func (cf *componentsFixture) goOffline(t *testing.T) {
	t.Helper()
	cf.g.ws.Close()
	deadline := time.Now().Add(soon)
	for {
		res := cf.run(t)
		if res.status == http.StatusConflict {
			return
		}
		if res.status != http.StatusAccepted || time.Now().After(deadline) {
			t.Fatalf("still online: %d %s", res.status, res.body)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// noteEvent is an interaction_response as the member's sessions get it.
type noteEvent struct {
	ID         uuid.UUID        `json:"id"`
	ChannelID  uuid.UUID        `json:"channel_id"`
	Content    string           `json:"content"`
	Components []core.ActionRow `json:"components"`
	Deleted    bool             `json:"deleted"`
}

func expectNote(t *testing.T, ws *wsConn) noteEvent {
	t.Helper()
	p, ok := ws.expect("interaction_response", soon)
	if !ok {
		t.Fatal("no interaction_response")
	}
	var n noteEvent
	if err := json.Unmarshal(p, &n); err != nil {
		t.Fatal(err)
	}
	return n
}

// modalEvent is an interaction_modal as the member's sessions get it.
type modalEvent struct {
	ID         uuid.UUID        `json:"id"`
	ChannelID  uuid.UUID        `json:"channel_id"`
	BotID      uuid.UUID        `json:"bot_id"`
	Title      string           `json:"title"`
	Components []core.ActionRow `json:"components"`
}

func expectModal(t *testing.T, ws *wsConn) modalEvent {
	t.Helper()
	p, ok := ws.expect("interaction_modal", soon)
	if !ok {
		t.Fatal("no interaction_modal")
	}
	var m modalEvent
	if err := json.Unmarshal(p, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// feedbackModal is the modal of doc/bots.md.
func feedbackModal() map[string]any {
	return map[string]any{"custom_id": "feedback", "title": "Feedback", "components": []map[string]any{
		{"type": 1, "components": []map[string]any{{"type": 4, "custom_id": "subject", "label": "Subject", "style": 1, "min_length": 2, "max_length": 10}}},
		{"type": 1, "components": []map[string]any{{"type": 4, "custom_id": "body", "label": "What happened?", "style": 2, "required": false}}},
	}}
}

func submitPath(ch, modal uuid.UUID) string {
	return "/api/channels/" + ch.String() + "/modals/" + modal.String()
}
