//go:build integration

package server

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat/expiry"
	"github.com/msch128/mnema-talk/internal/events"
)

// Messages with a lifetime (doc/timed-messages.md): the bounds and settings,
// the end as an own delete, and what a message with a lifetime cannot be.

// newTimedApp is newApp without the expiry sweeper, so a test sees an ended
// message the sweep has not removed and runs the sweep itself.
func newTimedApp(t *testing.T) *app {
	t.Helper()
	return newAppWithDeps(t, false, nil, func(d *Deps) { d.HoldExpiry = true })
}

type timedMessage struct {
	ID        uuid.UUID  `json:"id"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt *time.Time `json:"expires_at"`
	ReplyToID *uuid.UUID `json:"reply_to_id"`
	ReplyTo   *struct {
		Deleted bool `json:"deleted"`
	} `json:"reply_to"`
}

// sendTimed posts body to the channel and answers the stored message.
func sendTimed(t *testing.T, c *client, ch uuid.UUID, body map[string]any) timedMessage {
	t.Helper()
	res := c.post("/api/channels/"+ch.String()+"/messages", body)
	wantStatus(t, "post with a lifetime", res, http.StatusCreated)
	var m timedMessage
	res.decode(t, &m)
	return m
}

// endLifetime moves the messages' lifetimes into the past as if they had
// run out a second ago (a lifetime cannot be changed through the app: the
// trigger is lifted for this one statement).
func (a *app) endLifetime(ids ...uuid.UUID) {
	a.t.Helper()
	tx, err := a.db.Begin(context.Background())
	if err != nil {
		a.t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	for _, q := range []string{
		`ALTER TABLE messages DISABLE TRIGGER message_lifetime_is_fixed`,
		`UPDATE messages SET created_at = created_at - (expires_at - now()) - interval '1 second',
		        expires_at = now() - interval '1 second'
		 WHERE id = ANY($1) AND expires_at IS NOT NULL`,
		`ALTER TABLE messages ENABLE TRIGGER message_lifetime_is_fixed`,
	} {
		args := []any{}
		if strings.Contains(q, "$1") {
			args = append(args, ids)
		}
		if _, err := tx.Exec(context.Background(), q, args...); err != nil {
			a.t.Fatal(err)
		}
	}
	if err := tx.Commit(context.Background()); err != nil {
		a.t.Fatal(err)
	}
}

// endLifetimeInPlace ends the lifetimes a second ago and keeps the messages'
// times, for a test whose other rows depend on when a message was written (the
// store trace's read markers). The bounds check is lifted for this one
// statement and put back as it was (NOT VALID, migration 0106).
func (a *app) endLifetimeInPlace(ids ...uuid.UUID) {
	a.t.Helper()
	ctx := context.Background()
	tx, err := a.db.Begin(ctx)
	if err != nil {
		a.t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, q := range []string{
		`ALTER TABLE messages DISABLE TRIGGER message_lifetime_is_fixed`,
		`ALTER TABLE messages DROP CONSTRAINT messages_lifetime_bounds`,
		`UPDATE messages SET expires_at = now() - interval '1 second' WHERE id = ANY($1) AND expires_at IS NOT NULL`,
		`ALTER TABLE messages ADD CONSTRAINT messages_lifetime_bounds
		 CHECK (expires_at IS NULL OR expires_at - created_at BETWEEN interval '60 seconds' AND interval '30 days') NOT VALID`,
		`ALTER TABLE messages ENABLE TRIGGER message_lifetime_is_fixed`,
	} {
		args := []any{}
		if strings.Contains(q, "$1") {
			args = append(args, ids)
		}
		if _, err := tx.Exec(ctx, q, args...); err != nil {
			a.t.Fatal(err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		a.t.Fatal(err)
	}
}

// sweepEnded runs one pass of the expiry sweeper.
func (a *app) sweepEnded() {
	a.t.Helper()
	s := expiry.New(expiry.Config{DB: a.db, Events: a.events, Objects: a.store})
	if _, err := s.Sweep(context.Background()); err != nil {
		a.t.Fatal(err)
	}
}

type timedWorld struct {
	a            *app
	owner, alice *client
	bob          *client
	ch           uuid.UUID
}

func newTimedWorld(t *testing.T) *timedWorld {
	t.Helper()
	a := newTimedApp(t)
	w := &timedWorld{a: a, owner: a.seedAdmin()}
	w.alice, w.bob = a.register(w.owner, "alice"), a.register(w.owner, "bob")
	w.ch = a.createChannel(w.owner, "general", "text")
	return w
}

func (w *timedWorld) path(rest string) string { return "/api/channels/" + w.ch.String() + rest }

func TestAMessageWithALifetimeEndsLikeItsAuthorsDelete(t *testing.T) {
	w := newTimedWorld(t)
	a := w.a
	m := sendTimed(t, w.alice, w.ch, map[string]any{"content": "gone in a minute", "lifetime_seconds": 60})
	if m.ExpiresAt == nil || m.ExpiresAt.Sub(m.CreatedAt) != time.Minute {
		t.Fatalf("expires_at %v, created_at %v: want the server's time of storing plus 60 s", m.ExpiresAt, m.CreatedAt)
	}
	reply := sendTimed(t, w.bob, w.ch, map[string]any{"content": "a reply in its thread", "parent_id": m.ID})
	quote := sendTimed(t, w.bob, w.ch, map[string]any{"content": "quoting it", "reply_to_id": m.ID})
	wantStatus(t, "save", w.bob.put("/api/me/saved/"+m.ID.String(), map[string]any{}), http.StatusOK)
	wantStatus(t, "react", w.bob.post("/api/messages/"+m.ID.String()+"/reactions", map[string]string{"emoji": "👍"}), http.StatusOK)
	a.events.Take()

	a.endLifetime(m.ID)
	a.sweepEnded()

	for name, id := range map[string]uuid.UUID{"the message": m.ID, "its thread reply": reply.ID} {
		if a.countRows(`SELECT COUNT(*) FROM messages WHERE id = $1`, id) != 0 {
			t.Errorf("%s is still stored", name)
		}
	}
	if a.countRows(`SELECT COUNT(*) FROM messages WHERE id = $1`, quote.ID) != 1 {
		t.Error("a message that only quoted it was removed too")
	}
	for name, q := range map[string]string{
		"saved entry": `SELECT COUNT(*) FROM saved_messages WHERE message_id = $1`,
		"reaction":    `SELECT COUNT(*) FROM message_reactions WHERE message_id = $1`,
		"audit entry": `SELECT COUNT(*) FROM audit_log WHERE target_id = $1::text`,
	} {
		if n := a.countRows(q, m.ID); n != 0 {
			t.Errorf("%s: %d left", name, n)
		}
	}
	var deleted int
	for _, e := range a.events.Snapshot() {
		if e.Type == events.MessageDelete {
			deleted++
		}
	}
	if deleted == 0 {
		t.Error("no message_delete was published at the end")
	}
	var got timedMessage
	w.bob.get(w.path("/messages")).decodeFirstWith(t, quote.ID, &got)
	if got.ReplyToID == nil || got.ReplyTo == nil || !got.ReplyTo.Deleted {
		t.Errorf("the quote should show the original as deleted: %+v", got)
	}
}

// decodeFirstWith decodes the element of a JSON array with the given id.
func (r response) decodeFirstWith(t *testing.T, id uuid.UUID, v *timedMessage) {
	t.Helper()
	var list []timedMessage
	r.decode(t, &list)
	for _, m := range list {
		if m.ID == id {
			*v = m
			return
		}
	}
	t.Fatalf("message %s not in the list", id)
}

func TestALifetimeStaysWithinItsBoundsAndTheCommunitysSettings(t *testing.T) {
	w := newTimedWorld(t)
	post := func(seconds any) response {
		return w.alice.post(w.path("/messages"), map[string]any{"content": "timed", "lifetime_seconds": seconds})
	}
	for _, s := range []any{59, 0, -1, 2592001} {
		wantStatus(t, "out of bounds", post(s), http.StatusBadRequest)
	}
	wantStatus(t, "shortest", post(60), http.StatusCreated)
	wantStatus(t, "longest", post(2592000), http.StatusCreated)

	wantStatus(t, "a member sets nothing", w.alice.put("/api/admin/message-settings/lifetimes", map[string]any{"message_lifetimes_enabled": false}), http.StatusForbidden)
	wantStatus(t, "ceiling", w.owner.put("/api/admin/message-settings/lifetimes", map[string]any{"message_lifetime_max_seconds": 120}), http.StatusOK)
	wantStatus(t, "beyond the ceiling", post(121), http.StatusBadRequest)
	wantStatus(t, "within the ceiling", post(120), http.StatusCreated)
	wantStatus(t, "the Owner too", w.owner.post(w.path("/messages"), map[string]any{"content": "x", "lifetime_seconds": 121}), http.StatusBadRequest)

	wantStatus(t, "off", w.owner.put("/api/admin/message-settings/lifetimes", map[string]any{"message_lifetimes_enabled": false}), http.StatusOK)
	wantStatus(t, "refused while off", post(60), http.StatusBadRequest)
	wantStatus(t, "plain messages still go", w.alice.post(w.path("/messages"), map[string]any{"content": "plain"}), http.StatusCreated)
	var s struct {
		Enabled bool `json:"message_lifetimes_enabled"`
		Max     int  `json:"message_lifetime_max_seconds"`
	}
	w.alice.get("/api/message-settings").decode(t, &s)
	if s.Enabled || s.Max != 120 {
		t.Errorf("member view of the settings: %+v", s)
	}
	if n := w.a.countRows(`SELECT COUNT(*) FROM audit_log WHERE action = 'settings.message_lifetimes_set'`); n != 2 {
		t.Errorf("audit entries of the lifetime settings: %d", n)
	}
	wantStatus(t, "ceiling out of bounds", w.owner.put("/api/admin/message-settings/lifetimes", map[string]any{"message_lifetime_max_seconds": 30}), http.StatusBadRequest)
}

func TestAMessageWithALifetimeIsNeverPinnedForwardedOrExtended(t *testing.T) {
	w := newTimedWorld(t)
	m := sendTimed(t, w.alice, w.ch, map[string]any{"content": "short-lived", "lifetime_seconds": 3600})
	pin := w.owner.put(w.path("/messages/"+m.ID.String()+"/pin"), nil)
	wantStatus(t, "pin", pin, http.StatusBadRequest)
	if !strings.Contains(string(pin.body), "cannot be pinned") {
		t.Errorf("the refusal does not say why: %s", pin.body)
	}
	other := w.a.createChannel(w.owner, "other", "text")
	wantStatus(t, "forward", w.alice.forward(m.ID, other, ""), http.StatusBadRequest)
	// An edit changes the text, never the lifetime: the field is not taken.
	wantStatus(t, "edit with a lifetime", w.alice.put(w.path("/messages/"+m.ID.String()), map[string]any{"content": "edited", "lifetime_seconds": 60}), http.StatusBadRequest)
	wantStatus(t, "edit", w.alice.put(w.path("/messages/"+m.ID.String()), map[string]any{"content": "edited"}), http.StatusOK)
	if n := w.a.countRows(`SELECT COUNT(*) FROM messages WHERE id = $1 AND expires_at = $2`, m.ID, *m.ExpiresAt); n != 1 {
		t.Error("the edit moved the end")
	}
	if _, err := w.a.db.Exec(context.Background(), `UPDATE messages SET expires_at = NULL WHERE id = $1`, m.ID); err == nil {
		t.Error("the database let the lifetime be cleared")
	}
	if _, err := w.a.db.Exec(context.Background(), `UPDATE messages SET is_pinned = TRUE WHERE id = $1`, m.ID); err == nil {
		t.Error("the database let a message with a lifetime be pinned")
	}
	reply := w.bob.post(w.path("/messages"), map[string]any{"content": "answer", "reply_to_id": m.ID})
	wantStatus(t, "replying to it", reply, http.StatusCreated)
}
