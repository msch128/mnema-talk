//go:build integration

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// unchanged reports whether msg still has its text and its two action rows.
func unchanged(a *app, msg core.Message) bool {
	return a.count(`SELECT COUNT(*) FROM messages m JOIN message_components c ON c.message_id = m.id
		WHERE m.id = $1 AND m.content = $2 AND jsonb_array_length(c.components) = 2`, msg.ID, msg.Content) == 1
}

// failComponentWrites makes every write of message_components fail until the
// test ends, as a database that refuses the second table of a post would.
func failComponentWrites(t *testing.T, a *app) {
	t.Helper()
	a.runSQL(`CREATE FUNCTION fail_components() RETURNS trigger AS $$ BEGIN RAISE EXCEPTION 'boom'; END $$ LANGUAGE plpgsql`)
	a.runSQL(`CREATE TRIGGER fail_components BEFORE INSERT OR UPDATE OR DELETE ON message_components FOR EACH ROW EXECUTE FUNCTION fail_components()`)
	t.Cleanup(func() {
		_, _ = a.db.Exec(context.Background(), `DROP TRIGGER IF EXISTS fail_components ON message_components; DROP FUNCTION IF EXISTS fail_components()`)
	})
}

// A bot message and its components are written together: when storing or
// removing the components fails, the post leaves no message and the edit
// leaves the message as it was.
func TestAFailedWriteOfTheComponentsWritesNothingElse(t *testing.T) {
	f := newBotFixture(t, false)
	f.a.router.Bots.SetLimits(1000, 1000)
	msg := f.postWithComponents(t, f.open, "Deploy?", deployRows())
	failComponentWrites(t, f.a)

	before := f.a.count(`SELECT COUNT(*) FROM messages`)
	res := f.client.post(botPath(f.open, "/messages"), map[string]any{"content": "Again?", "components": againRow()})
	wantStatus(t, "post", res, http.StatusInternalServerError)
	if n := f.a.count(`SELECT COUNT(*) FROM messages`); n != before {
		t.Fatalf("a message without its components was stored: %d, before %d", n, before)
	}
	path := botPath(f.open, "/messages/"+msg.ID.String())
	wantStatus(t, "remove", f.client.patch(path, map[string]any{"content": "Done", "components": []any{}}), http.StatusInternalServerError)
	if !unchanged(f.a, msg) {
		t.Fatal("a failed removal changed the message")
	}
}

// PATCH /api/bot/v1/channels/{id}/messages/{id} edits only the bot's own
// message of that channel, and only with a body it can read; a refused edit
// leaves the message as it was.
func TestABotEditsOnlyItsOwnMessageOfThatChannel(t *testing.T) {
	f := newBotFixture(t, false)
	f.a.router.Bots.SetLimits(1000, 1000)
	side := f.a.createChannel(f.owner, "side", "text")
	wantStatus(t, "scope", putOverwrites(f.owner, "channels", side, overwrite("role", f.roleID, perm.ViewChannel, 0)), http.StatusNoContent)
	msg := f.postWithComponents(t, f.open, "Deploy?", deployRows())
	path := botPath(f.open, "/messages/"+msg.ID.String())

	for name, body := range map[string]string{
		"not JSON":                `{"content":`,
		"components not a list":   `{"components":{"type":1}}`,
		"a component row invalid": `{"components":[{"type":1,"components":[{"type":2,"style":1,"label":"x"}]}]}`,
		"content too long":        `{"content":"` + strings.Repeat("a", core.MaxMessageLen+1) + `"}`,
		"nothing left to show":    `{"content":"","components":[]}`,
	} {
		res := f.client.req(http.MethodPatch, path, json.RawMessage(body), nil)
		wantStatus(t, name, res, http.StatusBadRequest)
	}
	res := f.client.patch(botPath(side, "/messages/"+msg.ID.String()), map[string]any{"content": "moved"})
	wantStatus(t, "the message through another channel", res, http.StatusNotFound)
	if e := decodeDiscord(t, res); e.Code != httpx.DiscordCodeUnknownMessage {
		t.Fatalf("another channel: %s", res.body)
	}
	members := f.a.send(f.owner, f.open, "the Owner's words")
	wantStatus(t, "a member's message", f.client.patch(botPath(f.open, "/messages/"+members.String()), map[string]any{"content": "mine now", "components": []any{}}), http.StatusForbidden)
	if f.a.count(`SELECT COUNT(*) FROM message_components WHERE message_id = $1`, members) != 0 {
		t.Fatal("components landed on a member's message")
	}
	if !unchanged(f.a, msg) {
		t.Fatal("a refused edit changed the message")
	}

	// Without Send messages in the channel the bot edits nothing there.
	wantStatus(t, "no send", putOverwrites(f.owner, "channels", f.open, overwrite("role", f.roleID, perm.ViewChannel, perm.SendMessages)), http.StatusNoContent)
	wantStatus(t, "edit without Send messages", f.client.patch(path, map[string]any{"content": "still?"}), http.StatusForbidden)
}

// noteShown is an interaction_response with the embeds of the note.
type noteShown struct {
	noteEvent
	Embeds []core.Embed `json:"embeds"`
}

// An ephemeral note is edited (callback 7 on its click, PATCH @original of
// its interaction) only while the bot may still send in the channel and only
// into something it can show; its embeds reach the member.
func TestANoteIsEditedOnlyWhileTheBotMaySendAndIntoSomethingToShow(t *testing.T) {
	cf := newComponentsFixture(t)
	in := cf.command(t)
	wantStatus(t, "note", cf.callback(in, 4, map[string]any{"content": "Choose", "flags": 64, "components": againRow()}), http.StatusNoContent)
	note := expectNote(t, cf.maxWS)
	original := "/messages/@original"

	wantStatus(t, "an empty note", cf.answer(in, http.MethodPatch, original, map[string]any{"content": "", "components": []any{}}), http.StatusBadRequest)
	res := cf.answer(in, http.MethodPatch, original, map[string]any{"content": "", "embeds": []map[string]any{{"title": "Status", "description": "All green"}}})
	wantStatus(t, "embeds only", res, http.StatusNoContent)
	p, ok := cf.maxWS.expect("interaction_response", soon)
	var shown noteShown
	if !ok || json.Unmarshal(p, &shown) != nil || shown.ID != note.ID || shown.Content != "" ||
		len(shown.Embeds) != 1 || shown.Embeds[0].Title != "Status" || len(shown.Components) != 1 {
		t.Fatalf("note with embeds: %s", p)
	}

	notePath := "/api/channels/" + cf.open.String() + "/ephemeral/" + note.ID.String() + "/components"
	wantStatus(t, "click", cf.max.post(notePath, map[string]any{"custom_id": "again"}), http.StatusAccepted)
	clicked := cf.interaction(t)
	wantStatus(t, "no send", putOverwrites(cf.owner, "channels", cf.open, overwrite("role", cf.roleID, perm.ViewChannel, perm.SendMessages)), http.StatusNoContent)
	wantStatus(t, "callback 7 without Send messages", cf.callback(clicked, 7, map[string]any{"content": "Chosen"}), http.StatusForbidden)
	wantStatus(t, "@original without Send messages", cf.answer(in, http.MethodPatch, original, map[string]any{"content": "Chosen"}), http.StatusForbidden)
	if p, ok := cf.maxWS.expect("interaction_response", none); ok {
		t.Fatalf("a refused edit reached the member: %s", p)
	}
	// The refused callback left the click unanswered.
	wantStatus(t, "send again", putOverwrites(cf.owner, "channels", cf.open, overwrite("role", cf.roleID, perm.ViewChannel, 0)), http.StatusNoContent)
	wantStatus(t, "callback 7", cf.callback(clicked, 7, map[string]any{"content": "Chosen"}), http.StatusNoContent)
	if n := expectNote(t, cf.maxWS); n.ID != note.ID || n.Content != "Chosen" {
		t.Fatalf("updated note: %+v", n)
	}
}

// A click on a message whose bot can no longer view the channel cannot
// update it: the message is unknown to the bot.
func TestAClickCannotUpdateAMessageInAChannelItsBotLostSightOf(t *testing.T) {
	cf := newComponentsFixture(t)
	m := cf.postWithComponents(t, cf.open, "Deploy?", deployRows())
	wantStatus(t, "click", click(cf.max, cf.open, m.ID, "cancel"), http.StatusAccepted)
	in := cf.interaction(t)
	wantStatus(t, "hide", putOverwrites(cf.owner, "channels", cf.open, overwrite("role", everyoneID(t, cf.owner), 0, perm.ViewChannel)), http.StatusNoContent)
	wantStatus(t, "update unseen", cf.callback(in, 7, map[string]any{"content": "x"}), http.StatusNotFound)
	if n := cf.a.count(`SELECT COUNT(*) FROM messages WHERE id = $1 AND content = 'Deploy?'`, m.ID); n != 1 {
		t.Fatal("updated anyway")
	}
}
