//go:build integration

package push

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
)

func (e *env) remind(m member, channel *uuid.UUID) error {
	return e.svc.SendEventReminder(context.Background(), m.id, uuid.New(), "Game night", time.Now().Add(15*time.Minute), channel)
}

func TestAnEventReminderReachesTheMembersDevices(t *testing.T) {
	e := newEnv(t, http.StatusCreated)
	bob := e.member("bob")
	eventID := uuid.New()

	if err := e.svc.SendEventReminder(context.Background(), bob.id, eventID, "Game night", time.Now(), &e.general); err != nil {
		t.Fatal(err)
	}
	got := e.pushedTo("/device/bob", 1)
	p := e.content(bob, got[0])
	if p.Type != typeEvent || p.Title != "Game night" || p.Tag != "event:"+eventID.String() || p.Body == "" {
		t.Fatalf("payload: %+v", p)
	}
	if p.ChannelID == nil || *p.ChannelID != e.general {
		t.Fatalf("channel: %v", p.ChannelID)
	}

	// An event outside any Talk has no channel to consult.
	if err := e.remind(bob, nil); err != nil {
		t.Fatal(err)
	}
	e.pushedTo("/device/bob", 2)
}

func TestAnEventReminderIsHeldBackByQuietPresenceAndMutes(t *testing.T) {
	tests := []struct {
		name  string
		setup string
	}{
		{"do not disturb", `UPDATE users SET presence = 'dnd' WHERE id = $1`},
		{"focus", `UPDATE users SET presence = 'focus' WHERE id = $1`},
		{"muted channel", `INSERT INTO channel_reads (user_id, channel_id, notify_level) SELECT $1, id, 'mute' FROM channels WHERE name = 'general'`},
		{"muted category", `INSERT INTO category_notify (user_id, category_id, notify_level) SELECT $1, id, 'mute' FROM categories`},
		{"muted server", `INSERT INTO server_notify (user_id, notify_level) VALUES ($1, 'mute')`},
		{"server muted for an hour", `INSERT INTO server_notify (user_id, notify_level, muted_until) VALUES ($1, 'mute', NOW() + INTERVAL '1 hour')`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t, http.StatusCreated)
			bob := e.member("bob")
			e.exec(tc.setup, bob.id)
			if err := e.remind(bob, &e.general); err != nil {
				t.Fatal(err)
			}
			settle()
			if got := e.requestsTo("/device/bob"); len(got) != 0 {
				t.Fatalf("%d pushes, want none", len(got))
			}
		})
	}
}

func TestAnEventReminderFollowsTheMostSpecificNotifySetting(t *testing.T) {
	e := newEnv(t, http.StatusCreated)
	bob := e.member("bob")
	e.exec(`INSERT INTO server_notify (user_id, notify_level) VALUES ($1, 'mute')`, bob.id)
	e.exec(`INSERT INTO channel_reads (user_id, channel_id, notify_level) VALUES ($1, $2, 'all')`, bob.id, e.general)
	if err := e.remind(bob, &e.general); err != nil {
		t.Fatal(err)
	}
	e.pushedTo("/device/bob", 1)

	// A server mute that ended no longer counts.
	e.exec(`UPDATE channel_reads SET notify_level = 'inherit' WHERE user_id = $1`, bob.id)
	e.exec(`UPDATE server_notify SET notify_level = 'mute', muted_until = NOW() - INTERVAL '1 minute' WHERE user_id = $1`, bob.id)
	if err := e.remind(bob, &e.general); err != nil {
		t.Fatal(err)
	}
	e.pushedTo("/device/bob", 2)
}

func TestAnEventReminderEdgeCases(t *testing.T) {
	e := newEnv(t, http.StatusCreated)
	bob := e.member("bob")
	nobody := member{id: uuid.New()}
	if err := e.remind(nobody, nil); err == nil {
		t.Fatal("an unknown account must be reported")
	}

	// No registered browser: nothing to send, nothing wrong.
	e.exec(`DELETE FROM push_subscriptions WHERE user_id = $1`, bob.id)
	if err := e.remind(bob, &e.general); err != nil {
		t.Fatal(err)
	}
	settle()
	if got := e.requestsTo("/device/bob"); len(got) != 0 {
		t.Fatalf("%d pushes without a subscription", len(got))
	}

	// A service without a sender (push not configured) and a nil service do nothing.
	if err := (&Service{DB: e.pool}).SendEventReminder(context.Background(), bob.id, uuid.New(), "x", time.Now(), nil); err != nil {
		t.Fatal(err)
	}
	var none *Service
	if err := none.SendEventReminder(context.Background(), bob.id, uuid.New(), "x", time.Now(), nil); err != nil {
		t.Fatal(err)
	}
}

func TestAnEventReminderHidesTheTitleWithoutPreviews(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(e *env, m member)
	}{
		{"member opted out", func(e *env, m member) { e.exec(`UPDATE users SET push_preview = FALSE WHERE id = $1`, m.id) }},
		{"operator forbids", func(e *env, _ member) { e.svc.Previews = false }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t, http.StatusCreated)
			bob := e.member("bob")
			tc.setup(e, bob)
			if err := e.remind(bob, &e.general); err != nil {
				t.Fatal(err)
			}
			p := e.content(bob, e.pushedTo("/device/bob", 1)[0])
			if p.Title == "Game night" || p.Body == "" {
				t.Fatalf("payload shows the event title: %+v", p)
			}
		})
	}
}

func TestAnEventReminderForAMissingTalkSendsNothing(t *testing.T) {
	e := newEnv(t, http.StatusCreated)
	bob := e.member("bob")
	gone := uuid.New()
	if err := e.remind(bob, &gone); err != nil {
		t.Fatal(err)
	}
	settle()
	if got := e.requestsTo("/device/bob"); len(got) != 0 {
		t.Fatalf("%d pushes, want none", len(got))
	}
}
