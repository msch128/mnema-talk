//go:build integration

package server

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// A bot's REST edit takes allowed_mentions like callback 7 and @original do,
// and an edit that sends it counts towards the per-channel post limit.
func TestABotEditTakesAllowedMentionsAndCountsAsAPostWhenItDoes(t *testing.T) {
	f := newBotFixture(t, false)
	f.a.register(f.owner, "max")
	var maxID uuid.UUID
	if err := f.a.db.QueryRow(t.Context(), `SELECT id FROM users WHERE username = 'max'`).Scan(&maxID); err != nil {
		t.Fatal(err)
	}
	res := f.client.post(botPath(f.open, "/messages"), map[string]any{"content": "draft"})
	wantStatus(t, "post", res, http.StatusCreated)
	msg := messageOf(t, res.body)
	edit := botPath(f.open, "/messages/"+msg.ID.String())

	res = f.client.patch(edit, map[string]any{"content": "hi @max", "allowed_mentions": map[string]any{"parse": []string{}}})
	wantStatus(t, "edit with parse []", res, http.StatusOK)
	if mentioned(t, res.body, maxID) {
		t.Fatalf("parse [] notified max: %s", res.body)
	}
	res = f.client.patch(edit, map[string]any{"content": "hi again @max"})
	wantStatus(t, "edit without", res, http.StatusOK)
	if !mentioned(t, res.body, maxID) {
		t.Fatalf("an edit without allowed_mentions notifies the members it names: %s", res.body)
	}

	// The post above and the edit with allowed_mentions took 2 of the 5 posts
	// per 5 seconds; three more posts fill the window.
	for i := 0; i < 3; i++ {
		wantStatus(t, "post", f.client.say(f.open, "fill"), http.StatusCreated)
	}
	wantStatus(t, "an edit that may notify, over the limit", f.client.patch(edit, map[string]any{"content": "x", "allowed_mentions": map[string]any{"parse": []string{"users"}}}), http.StatusTooManyRequests)
	wantStatus(t, "a plain edit is not a post", f.client.patch(edit, map[string]any{"content": "y"}), http.StatusOK)
}
