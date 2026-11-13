//go:build integration

package server

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/bot"
	"github.com/msch128/mnema-talk/internal/chat/core"
)

func againRow() []map[string]any {
	return []map[string]any{{"type": 1, "components": []map[string]any{{"type": 2, "style": 1, "label": "Again", "custom_id": "again"}}}}
}

// The message a component is on is the click's original response: callback 7
// updates it, PATCH @original edits it, DELETE @original deletes it.
func TestAClickUpdatesTheMessageItIsOn(t *testing.T) {
	cf := newComponentsFixture(t)
	m := cf.postWithComponents(t, cf.open, "Deploy?", deployRows())
	cf.maxWS.expect("message_create", soon)
	original := "/messages/@original"

	wantStatus(t, "click", click(cf.max, cf.open, m.ID, "deploy:v2.4"), http.StatusAccepted)
	in := cf.interaction(t)
	wantStatus(t, "@original before an answer", cf.answer(in, http.MethodPatch, original, map[string]any{"content": "x"}), http.StatusConflict)
	wantStatus(t, "update", cf.callback(in, 7, map[string]any{"content": "Deploying v2.4", "components": againRow()}), http.StatusNoContent)
	p, ok := cf.maxWS.expect("message_update", soon)
	if got := messageOf(t, p); !ok || got.ID != m.ID || got.Content != "Deploying v2.4" || strings.Join(componentIDs(got.Components), ",") != "again" {
		t.Fatalf("message_update: %s", p)
	}
	wantStatus(t, "answered twice", cf.callback(in, 6, nil), http.StatusConflict)
	res := cf.answer(in, http.MethodPatch, original, map[string]any{"content": "Deployed"})
	wantStatus(t, "edit @original", res, http.StatusOK)
	if got := messageOf(t, res.body); got.ID != m.ID || got.Content != "Deployed" || len(got.Components) != 1 {
		t.Fatalf("@original: %s", res.body)
	}
	wantStatus(t, "delete @original", cf.answer(in, http.MethodDelete, original, nil), http.StatusNoContent)
	if n := cf.a.count(`SELECT COUNT(*) FROM messages WHERE id = $1`, m.ID); n != 0 {
		t.Fatal("@original not deleted")
	}
	wantStatus(t, "edit after delete", cf.answer(in, http.MethodPatch, original, map[string]any{"content": "x"}), http.StatusNotFound)

	// Callback 6 acknowledges; @original is still the component's message.
	m = cf.postWithComponents(t, cf.open, "Deploy?", deployRows())
	wantStatus(t, "click", click(cf.max, cf.open, m.ID, "cancel"), http.StatusAccepted)
	in = cf.interaction(t)
	wantStatus(t, "defer update", cf.callback(in, 6, nil), http.StatusNoContent)
	res = cf.answer(in, http.MethodPatch, original, map[string]any{"components": []any{}})
	wantStatus(t, "edit after 6", res, http.StatusOK)
	if got := messageOf(t, res.body); got.ID != m.ID || got.Components != nil || got.Content != "Deploy?" {
		t.Fatalf("after 6: %s", res.body)
	}

	// Callback 4 answers with a new message, which is then @original.
	m = cf.postWithComponents(t, cf.open, "Deploy?", deployRows())
	wantStatus(t, "click", click(cf.max, cf.open, m.ID, "cancel"), http.StatusAccepted)
	in = cf.interaction(t)
	wantStatus(t, "reply", cf.callback(in, 4, map[string]any{"content": "Cancelled", "components": againRow()}), http.StatusNoContent)
	res = cf.answer(in, http.MethodPatch, original, map[string]any{"content": "Cancelled for good"})
	wantStatus(t, "edit the reply", res, http.StatusOK)
	if got := messageOf(t, res.body); got.ID == m.ID || got.Content != "Cancelled for good" || len(got.Components) != 1 {
		t.Fatalf("reply: %s", res.body)
	}
}

func TestCallbackTypesFollowTheAllowedForTable(t *testing.T) {
	cf := newComponentsFixture(t)
	in := cf.command(t)
	for _, typ := range []int{6, 7} {
		res := cf.callback(in, typ, map[string]any{"content": "x"})
		wantStatus(t, "command answered with a click's callback", res, http.StatusBadRequest)
	}
	wantStatus(t, "unknown type", cf.callback(in, 8, nil), http.StatusBadRequest)
	wantStatus(t, "modal", cf.callback(in, 9, feedbackModal()), http.StatusNoContent)
	modal := expectModal(t, cf.maxWS)
	wantStatus(t, "follow-up after a modal", cf.answer(in, http.MethodPost, "/followups", map[string]any{"content": "x"}), http.StatusConflict)
	wantStatus(t, "@original of a modal", cf.answer(in, http.MethodPatch, "/messages/@original", map[string]any{"content": "x"}), http.StatusNotFound)

	// The submit of a command's modal takes 4 and 5, not 6, 7 or 9.
	wantStatus(t, "submit", cf.max.post(submitPath(cf.open, modal.ID), map[string]any{"values": map[string]string{"subject": "Login"}}), http.StatusAccepted)
	sub := cf.interaction(t)
	if sub.Type != bot.InteractionModalSubmit || sub.Message != nil {
		t.Fatalf("submit: %s", sub.raw)
	}
	for _, typ := range []int{6, 7} {
		wantStatus(t, "submit answered as a click", cf.callback(sub, typ, map[string]any{"content": "x"}), http.StatusBadRequest)
	}
	wantStatus(t, "modal for a modal", cf.callback(sub, 9, feedbackModal()), http.StatusBadRequest)
	wantStatus(t, "reply", cf.callback(sub, 4, map[string]any{"content": "Thanks"}), http.StatusNoContent)
	cf.maxWS.expect("message_create", soon)

	// A click takes every type: here 9, and its submit takes 7 on the click's message.
	m := cf.postWithComponents(t, cf.open, "Rate us", againRow())
	wantStatus(t, "click", click(cf.max, cf.open, m.ID, "again"), http.StatusAccepted)
	clicked := cf.interaction(t)
	wantStatus(t, "modal on a click", cf.callback(clicked, 9, feedbackModal()), http.StatusNoContent)
	modal = expectModal(t, cf.maxWS)
	wantStatus(t, "submit", cf.max.post(submitPath(cf.open, modal.ID), map[string]any{"values": map[string]string{"subject": "Great", "body": "line one\nline two"}}), http.StatusAccepted)
	sub = cf.interaction(t)
	if got := messageOf(t, sub.Message); got.ID != m.ID {
		t.Fatalf("submit of a click's modal carries the message: %s", sub.raw)
	}
	if len(sub.Data.Components) != 2 || sub.Data.Components[1].Components[0].Value != "line one\nline two" {
		t.Fatalf("submit data: %s", sub.raw)
	}
	wantStatus(t, "update on submit", cf.callback(sub, 7, map[string]any{"content": "Rated", "components": []any{}}), http.StatusNoContent)
	if p, ok := cf.maxWS.expect("message_update", soon); !ok || messageOf(t, p).Content != "Rated" {
		t.Fatalf("message_update: %s", p)
	}
}

// Callback 5 defers: the first PATCH of @original posts the answer and is
// one of the five messages; later ones edit it.
func TestTheFirstPatchAfterADeferralPostsTheAnswer(t *testing.T) {
	cf := newComponentsFixture(t)
	in := cf.command(t)
	wantStatus(t, "defer", cf.callback(in, 5, nil), http.StatusNoContent)
	wantStatus(t, "delete before posting", cf.answer(in, http.MethodDelete, "/messages/@original", nil), http.StatusNotFound)
	wantStatus(t, "nothing to post", cf.answer(in, http.MethodPatch, "/messages/@original", map[string]any{}), http.StatusBadRequest)
	res := cf.answer(in, http.MethodPatch, "/messages/@original", map[string]any{"content": "Here", "components": againRow()})
	wantStatus(t, "post", res, http.StatusOK)
	posted := messageOf(t, res.body)
	if posted.Content != "Here" || len(posted.Components) != 1 || posted.UserID != cf.bot.UserID {
		t.Fatalf("posted: %s", res.body)
	}
	cf.maxWS.expect("message_create", soon)
	res = cf.answer(in, http.MethodPatch, "/messages/@original", map[string]any{"content": "Here, edited"})
	wantStatus(t, "edit", res, http.StatusOK)
	if got := messageOf(t, res.body); got.ID != posted.ID || got.Content != "Here, edited" {
		t.Fatalf("edited: %s", res.body)
	}
	for i := 0; i < 4; i++ {
		wantStatus(t, "follow-up", cf.answer(in, http.MethodPost, "/followups", map[string]any{"content": "more"}), http.StatusCreated)
	}
	wantStatus(t, "a sixth message", cf.answer(in, http.MethodPost, "/followups", map[string]any{"content": "more"}), http.StatusConflict)
	wantStatus(t, "delete", cf.answer(in, http.MethodDelete, "/messages/@original", nil), http.StatusNoContent)
	if n := cf.a.count(`SELECT COUNT(*) FROM messages WHERE id = $1`, posted.ID); n != 0 {
		t.Fatal("not deleted")
	}

	// With flags 64 the answer is a note for the member, edited and deleted in place.
	in = cf.command(t)
	wantStatus(t, "defer ephemeral", cf.callback(in, 5, map[string]any{"flags": 64}), http.StatusNoContent)
	wantStatus(t, "post note", cf.answer(in, http.MethodPatch, "/messages/@original", map[string]any{"content": "Only you"}), http.StatusNoContent)
	note := expectNote(t, cf.maxWS)
	if note.Content != "Only you" || note.ID == uuid.Nil || note.ChannelID != cf.open {
		t.Fatalf("note: %+v", note)
	}
	wantStatus(t, "edit note", cf.answer(in, http.MethodPatch, "/messages/@original", map[string]any{"components": againRow()}), http.StatusNoContent)
	if n := expectNote(t, cf.maxWS); n.ID != note.ID || n.Content != "Only you" || len(n.Components) != 1 {
		t.Fatalf("replaced note: %+v", n)
	}
	wantStatus(t, "delete note", cf.answer(in, http.MethodDelete, "/messages/@original", nil), http.StatusNoContent)
	if n := expectNote(t, cf.maxWS); n.ID != note.ID || !n.Deleted {
		t.Fatalf("deleted note: %+v", n)
	}
	if strings.Contains(string(cf.owner.get("/api/channels/"+cf.open.String()+"/messages").body), "Only you") {
		t.Fatal("a note was stored")
	}
}

// An ephemeral note's components work for the member who sees it, as long as
// its interaction lives; the bot gets the note as the message.
func TestTheComponentsOfANoteWorkForItsMemberOnly(t *testing.T) {
	cf := newComponentsFixture(t)
	ownerWS := mustDial(t, cf.owner)
	in := cf.command(t)
	wantStatus(t, "reply", cf.callback(in, 4, map[string]any{"content": "Choose", "flags": 64, "components": againRow()}), http.StatusNoContent)
	note := expectNote(t, cf.maxWS)
	if len(note.Components) != 1 || note.Components[0].Components[0].CustomID != "again" {
		t.Fatalf("note: %+v", note)
	}
	if p, ok := ownerWS.expect("interaction_response", none); ok {
		t.Fatalf("someone else saw the note: %s", p)
	}
	notePath := func(ch, id uuid.UUID) string {
		return "/api/channels/" + ch.String() + "/ephemeral/" + id.String() + "/components"
	}
	body := map[string]any{"custom_id": "again"}
	wantStatus(t, "another member", cf.owner.post(notePath(cf.open, note.ID), body), http.StatusNotFound)
	wantStatus(t, "another channel", cf.max.post(notePath(cf.other, note.ID), body), http.StatusNotFound)
	wantStatus(t, "unknown note", cf.max.post(notePath(cf.open, uuid.New()), body), http.StatusNotFound)
	wantStatus(t, "unknown component", cf.max.post(notePath(cf.open, note.ID), map[string]any{"custom_id": "nope"}), http.StatusNotFound)
	wantStatus(t, "click", cf.max.post(notePath(cf.open, note.ID), body), http.StatusAccepted)
	clicked := cf.interaction(t)
	if clicked.Type != bot.InteractionMessageComponent || !strings.Contains(string(clicked.Message), `"flags":64`) ||
		!strings.Contains(string(clicked.Message), note.ID.String()) || !strings.Contains(string(clicked.Message), `"content":"Choose"`) {
		t.Fatalf("note click: %s", clicked.raw)
	}
	wantStatus(t, "update the note", cf.callback(clicked, 7, map[string]any{"content": "Chosen", "components": []any{}}), http.StatusNoContent)
	if n := expectNote(t, cf.maxWS); n.ID != note.ID || n.Content != "Chosen" || len(n.Components) != 0 {
		t.Fatalf("updated note: %+v", n)
	}
	wantStatus(t, "its component is gone", cf.max.post(notePath(cf.open, note.ID), body), http.StatusNotFound)
	wantStatus(t, "disable", cf.owner.patch(cf.botPath, map[string]any{"disabled": true}), http.StatusOK)
	wantStatus(t, "edit as a disabled bot", cf.answer(clicked, http.MethodPatch, "/messages/@original", map[string]any{"content": "x"}), http.StatusUnauthorized)
}

func TestAModalIsSubmittedOnceByItsMemberWithAnswersThatFit(t *testing.T) {
	cf := newComponentsFixture(t)
	ownerWS := mustDial(t, cf.owner)
	in := cf.command(t)
	wantStatus(t, "bad modal", cf.callback(in, 9, map[string]any{"custom_id": "m", "title": "T", "components": againRow()}), http.StatusBadRequest)
	wantStatus(t, "modal", cf.callback(in, 9, feedbackModal()), http.StatusNoContent)
	modal := expectModal(t, cf.maxWS)
	if modal.Title != "Feedback" || modal.ChannelID != cf.open || modal.BotID != cf.bot.ID || len(modal.Components) != 2 {
		t.Fatalf("modal: %+v", modal)
	}
	if p, ok := ownerWS.expect("interaction_modal", none); ok {
		t.Fatalf("someone else got the modal: %s", p)
	}
	submit := func(c *client, ch uuid.UUID, values map[string]string) response {
		return c.post(submitPath(ch, modal.ID), map[string]any{"values": values})
	}
	for name, res := range map[string]response{
		"too short":     submit(cf.max, cf.open, map[string]string{"subject": "a"}),
		"too long":      submit(cf.max, cf.open, map[string]string{"subject": strings.Repeat("a", 11)}),
		"required":      submit(cf.max, cf.open, map[string]string{"body": "only the optional one"}),
		"unknown input": submit(cf.max, cf.open, map[string]string{"subject": "Login", "extra": "x"}),
		"a line break":  submit(cf.max, cf.open, map[string]string{"subject": "Lo\ngin"}),
	} {
		wantStatus(t, name, res, http.StatusBadRequest)
	}
	wantStatus(t, "another member", submit(cf.owner, cf.open, map[string]string{"subject": "Login"}), http.StatusNotFound)
	wantStatus(t, "another channel", submit(cf.max, cf.other, map[string]string{"subject": "Login"}), http.StatusNotFound)
	wantStatus(t, "submit", submit(cf.max, cf.open, map[string]string{"subject": "  Login "}), http.StatusAccepted)
	sub := cf.interaction(t)
	if sub.Data.CustomID != "feedback" || len(sub.Data.Components) != 2 {
		t.Fatalf("submit: %s", sub.raw)
	}
	if first := sub.Data.Components[0].Components[0]; first.Type != core.ComponentTextInput || first.CustomID != "subject" || first.Value != "Login" {
		t.Fatalf("subject: %s", sub.raw)
	}
	if body := sub.Data.Components[1].Components[0]; body.CustomID != "body" || body.Value != "" {
		t.Fatalf("body: %s", sub.raw)
	}
	// An input left out is sent with an empty value, not without one.
	if !strings.Contains(string(sub.raw), `{"custom_id":"body","type":4,"value":""}`) {
		t.Fatalf("the empty answer lost its value: %s", sub.raw)
	}
	wantStatus(t, "twice", submit(cf.max, cf.open, map[string]string{"subject": "Login"}), http.StatusNotFound)

	// A submit the bot cannot take yet (offline) leaves the modal open.
	in = cf.command(t)
	wantStatus(t, "modal", cf.callback(in, 9, feedbackModal()), http.StatusNoContent)
	modal = expectModal(t, cf.maxWS)
	cf.goOffline(t)
	wantStatus(t, "offline", submit(cf.max, cf.open, map[string]string{"subject": "Login"}), http.StatusConflict)
	g := cf.dialGW("Bot " + cf.token.Secret)
	g.identify("", 0)
	wantStatus(t, "after reconnecting", submit(cf.max, cf.open, map[string]string{"subject": "Login"}), http.StatusAccepted)
	g.expect("INTERACTION_CREATE")
}
