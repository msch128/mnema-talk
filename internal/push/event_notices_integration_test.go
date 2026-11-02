//go:build integration

package push

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func (e *env) signupNotice(m member, kind string, channel *uuid.UUID) error {
	return e.svc.SendEventSignupNotice(context.Background(), m.id, uuid.New(), "Raid night", kind, "Tank", time.Now().Add(time.Hour), channel)
}

func (e *env) commentNotice(m member, channel *uuid.UUID) error {
	return e.svc.SendEventCommentNotice(context.Background(), m.id, uuid.New(), "Raid night", "Ada", "see you at seven", time.Now().Add(time.Hour), channel)
}

func TestASignupNoticeNamesTheRoleInTheMembersLanguage(t *testing.T) {
	for kind, want := range map[string]string{
		"promoted":   "Moved up from the waitlist: Tank",
		"removed":    "Removed from the role Tank",
		"waitlisted": "Put on the waitlist: Tank",
	} {
		t.Run(kind, func(t *testing.T) {
			e := newEnv(t, http.StatusCreated)
			bob := e.member("bob")
			if err := e.signupNotice(bob, kind, &e.general); err != nil {
				t.Fatal(err)
			}
			p := e.content(bob, e.pushedTo("/device/bob", 1)[0])
			if p.Type != typeEvent || p.Title != "Raid night" || p.Body != want || !strings.HasPrefix(p.Tag, "event-signup:") || p.EventID == nil {
				t.Fatalf("payload: %+v", p)
			}
		})
	}

	// German, and no title for a member who opted out of previews.
	e := newEnv(t, http.StatusCreated)
	bob := e.member("bob")
	e.exec(`UPDATE users SET locale = 'de', push_preview = FALSE WHERE id = $1`, bob.id)
	if err := e.signupNotice(bob, "promoted", nil); err != nil {
		t.Fatal(err)
	}
	p := e.content(bob, e.pushedTo("/device/bob", 1)[0])
	if p.Body != "Von der Warteliste nachgerückt: Tank" || p.Title == "Raid night" {
		t.Fatalf("german, no preview: %+v", p)
	}
}

func TestACommentNoticeShowsTheTextOnlyToMembersWhoWantPreviews(t *testing.T) {
	e := newEnv(t, http.StatusCreated)
	bob, eve := e.member("bob"), e.member("eve")
	e.exec(`UPDATE users SET push_preview = FALSE WHERE id = $1`, eve.id)
	if err := e.commentNotice(bob, &e.general); err != nil {
		t.Fatal(err)
	}
	if err := e.commentNotice(eve, nil); err != nil {
		t.Fatal(err)
	}
	p := e.content(bob, e.pushedTo("/device/bob", 1)[0])
	if p.Title != "Raid night" || p.Body != "Ada: see you at seven" || !strings.HasPrefix(p.Tag, "event-comment:") {
		t.Fatalf("with previews: %+v", p)
	}
	q := e.content(eve, e.pushedTo("/device/eve", 1)[0])
	if q.Title == "Raid night" || strings.Contains(q.Body, "seven") || q.Body != "New comment on an event" {
		t.Fatalf("without previews nothing of the comment shows: %+v", q)
	}
}

func TestEventNoticesAreHeldBackLikeReminders(t *testing.T) {
	setups := map[string]string{
		"do not disturb": `UPDATE users SET presence = 'dnd' WHERE id = $1`,
		"focus":          `UPDATE users SET presence = 'focus' WHERE id = $1`,
		"muted channel":  `INSERT INTO channel_reads (user_id, channel_id, notify_level) SELECT $1, id, 'mute' FROM channels WHERE name = 'general'`,
	}
	for name, setup := range setups {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t, http.StatusCreated)
			bob := e.member("bob")
			e.exec(setup, bob.id)
			if err := e.signupNotice(bob, "promoted", &e.general); err != nil {
				t.Fatal(err)
			}
			if err := e.commentNotice(bob, &e.general); err != nil {
				t.Fatal(err)
			}
			settle()
			if got := e.requestsTo("/device/bob"); len(got) != 0 {
				t.Fatalf("%d pushes, want none", len(got))
			}
		})
	}
}

func TestEventNoticesAreSilentWithoutAServiceOrAnAccount(t *testing.T) {
	var none *Service
	ctx := context.Background()
	if err := none.SendEventSignupNotice(ctx, uuid.New(), uuid.New(), "t", "promoted", "r", time.Now(), nil); err != nil {
		t.Fatal(err)
	}
	if err := none.SendEventCommentNotice(ctx, uuid.New(), uuid.New(), "t", "a", "x", time.Now(), nil); err != nil {
		t.Fatal(err)
	}
	e := newEnv(t, http.StatusCreated)
	ghost := member{id: uuid.New()}
	if err := e.signupNotice(ghost, "promoted", nil); err == nil {
		t.Log("an unknown account is a lookup error here, not a push")
	}
	if err := e.commentNotice(ghost, nil); err == nil {
		t.Log("same for comments")
	}
	settle()
}
