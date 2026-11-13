//go:build integration

package server

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/bot"
)

// mentioned reports whether the message in a response body lists user.
func mentioned(t *testing.T, body []byte, user uuid.UUID) bool {
	t.Helper()
	return slices.Contains(messageOf(t, body).Mentions, user)
}

// The answers to an interaction take allowed_mentions like a post: a callback,
// a follow-up and an edit of @original; @original refuses flags.
func TestInteractionAnswersTakeAllowedMentionsLikeAPost(t *testing.T) {
	cf := newComponentsFixture(t)
	var maxID uuid.UUID
	if err := cf.a.db.QueryRow(t.Context(), `SELECT id FROM users WHERE username = 'max'`).Scan(&maxID); err != nil {
		t.Fatal(err)
	}
	none := map[string]any{"parse": []string{}}

	in := cf.command(t)
	wantStatus(t, "callback 4 with allowed_mentions", cf.callback(in, 4, map[string]any{"content": "hi @max", "allowed_mentions": none}), http.StatusNoContent)
	p, _ := cf.maxWS.expect("message_create", soon)
	if mentioned(t, p, maxID) {
		t.Fatalf("parse [] still mentioned max: %s", p)
	}
	res := cf.answer(in, http.MethodPost, "/followups", map[string]any{"content": "again @max", "allowed_mentions": none})
	wantStatus(t, "follow-up with allowed_mentions", res, http.StatusCreated)
	if mentioned(t, res.body, maxID) {
		t.Fatalf("follow-up mentioned max: %s", res.body)
	}
	res = cf.answer(in, http.MethodPost, "/followups", map[string]any{"content": "and @max"})
	wantStatus(t, "follow-up without", res, http.StatusCreated)
	if !mentioned(t, res.body, maxID) {
		t.Fatalf("a follow-up without allowed_mentions notifies the members it names: %s", res.body)
	}
	wantStatus(t, "ephemeral follow-up", cf.answer(in, http.MethodPost, "/followups", map[string]any{"content": "psst", "flags": 64}), http.StatusNoContent)

	res = cf.answer(in, http.MethodPatch, "/messages/@original", map[string]any{"content": "edited @max", "allowed_mentions": none})
	wantStatus(t, "@original with allowed_mentions", res, http.StatusOK)
	if mentioned(t, res.body, maxID) {
		t.Fatalf("@original edit mentioned max: %s", res.body)
	}
	res = cf.answer(in, http.MethodPatch, "/messages/@original", map[string]any{"content": "x", "flags": 64})
	wantStatus(t, "@original with flags", res, http.StatusBadRequest)
	if e := decodeDiscord(t, res); !strings.Contains(e.Message, "flags cannot be set on @original") {
		t.Fatalf("@original flags refusal: %s", res.body)
	}

	// The deferred answer posted by the first PATCH follows allowed_mentions too.
	in = cf.command(t)
	wantStatus(t, "defer", cf.callback(in, 5, map[string]any{"flags": 0}), http.StatusNoContent)
	res = cf.answer(in, http.MethodPatch, "/messages/@original", map[string]any{"content": "later @max", "allowed_mentions": none})
	wantStatus(t, "post after defer", res, http.StatusOK)
	if mentioned(t, res.body, maxID) {
		t.Fatalf("deferred answer mentioned max: %s", res.body)
	}
}

// READY and GET /users/@me name the bot's handle, the name members mention,
// next to its display name.
func TestTheBotLearnsItsHandle(t *testing.T) {
	f := gwFixture(t)
	g := f.dialGW("Bot " + f.token.Secret)
	r := g.identify("", 0)
	var handle string
	if err := f.a.db.QueryRow(t.Context(), `SELECT username FROM users WHERE id = $1`, f.bot.UserID).Scan(&handle); err != nil {
		t.Fatal(err)
	}
	if r.User.Handle != handle || !strings.HasPrefix(handle, "bot-") || r.User.Username != "helper" {
		t.Fatalf("READY user: %+v, handle %q", r.User, handle)
	}
	var me bot.Self
	f.client.get(bot.BasePath+"/users/@me").decode(t, &me)
	if me.Handle != handle || me.Name != "helper" {
		t.Fatalf("@me: %+v", me)
	}
}
