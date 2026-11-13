//go:build integration

package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat/channels"
	"github.com/msch128/mnema-talk/internal/chat/messages"
	"github.com/msch128/mnema-talk/internal/testutil/racetest"
)

// The end of a message's lifetime released together with what members and
// moderators do to it, through the real routes. Each round stores a message
// whose lifetime ends a few milliseconds later; one caller is the sweeper's
// delete (retried until the end has passed), the other the action. Whatever
// order the database picks, the message is gone afterwards, the action
// answers a success or a clean refusal, and nothing it wrote outlives the
// message where the docs say it goes with it.

type timedRace struct {
	name string
	// fresh gives every round a new author (the action ends the account).
	fresh bool
	// freshBob gives every round a new acting member (the report and forward limits).
	freshBob bool
	act   func(f *timedRaceWorld, msg uuid.UUID, author *client) response
	// ok lists the answers the action may give.
	ok []int
	// left is an SQL count of rows that must not exist after the round ($1 = the message).
	left string
}

type timedRaceWorld struct {
	a          *app
	owner, bob *client
	ch, other  uuid.UUID
	// passwordHash is memberPassword's, for the members made per round.
	passwordHash string
}

var timedRaces = []timedRace{
	{name: "edit", act: func(f *timedRaceWorld, m uuid.UUID, au *client) response {
		return au.put("/api/channels/"+f.ch.String()+"/messages/"+m.String(), map[string]any{"content": "edited"})
	}, ok: []int{200, 404}, left: `SELECT COUNT(*) FROM message_edits WHERE message_id = $1`},
	{name: "thread reply", act: func(f *timedRaceWorld, m uuid.UUID, _ *client) response {
		return f.bob.post("/api/channels/"+f.ch.String()+"/messages", map[string]any{"content": "reply", "parent_id": m})
	}, ok: []int{201, 400}, left: `SELECT COUNT(*) FROM messages WHERE parent_id = $1`},
	{name: "pin", act: func(f *timedRaceWorld, m uuid.UUID, _ *client) response {
		return f.owner.put("/api/channels/"+f.ch.String()+"/messages/"+m.String()+"/pin", nil)
	}, ok: []int{400, 404}, left: `SELECT COUNT(*) FROM messages WHERE id = $1 AND is_pinned`},
	{name: "report", freshBob: true, act: func(f *timedRaceWorld, m uuid.UUID, _ *client) response {
		return f.bob.post("/api/messages/"+m.String()+"/report", map[string]any{"category": "spam"})
	}, ok: []int{201, 404}},
	{name: "forward", freshBob: true, act: func(f *timedRaceWorld, m uuid.UUID, _ *client) response {
		return f.bob.forward(m, f.other, "")
	}, ok: []int{400, 403}, left: `SELECT COUNT(*) FROM messages WHERE forwarded AND $1::uuid IS NOT NULL`},
	{name: "erasure of the author", fresh: true, act: func(f *timedRaceWorld, _ uuid.UUID, au *client) response {
		return f.a.deleteAccount(au, memberPassword, "")
	}, ok: []int{204}},
	{name: "ban of the author", fresh: true, act: func(f *timedRaceWorld, _ uuid.UUID, au *client) response {
		return f.owner.post("/api/admin/users/"+au.user.ID.String()+"/ban", map[string]any{"reason": "race"})
	}, ok: []int{200, 204}},
	{name: "auto-delete of the channel", act: func(f *timedRaceWorld, _ uuid.UUID, _ *client) response {
		d := &channels.AutoDeleter{DB: f.a.db, Events: f.a.events, Objects: f.a.store, Audit: &audit.Log{DB: f.a.db}}
		if err := d.Sweep(context.Background()); err != nil {
			return response{status: 500, body: []byte(err.Error())}
		}
		return response{status: 200}
	}, ok: []int{200}},
}

// member makes a member without the sign-up and login routes (their limits
// would stop a deep sweep that needs one per round).
func (f *timedRaceWorld) member(name string) *client {
	f.a.t.Helper()
	hash, err := f.hash()
	if err != nil {
		f.a.t.Fatal(err)
	}
	var id uuid.UUID
	if err := f.a.db.QueryRow(context.Background(), `INSERT INTO users (username, display_name, password_hash) VALUES ($1, $1, $2) RETURNING id`, name, hash).Scan(&id); err != nil {
		f.a.t.Fatal(err)
	}
	c := f.a.sessionFor(id)
	c.user = &auth.User{ID: id, Username: name}
	return c
}

// hash is memberPassword hashed once per world.
func (f *timedRaceWorld) hash() (string, error) {
	if f.passwordHash == "" {
		h, err := auth.HashPassword(memberPassword)
		if err != nil {
			return "", err
		}
		f.passwordHash = h
	}
	return f.passwordHash, nil
}

// sweepUntilGone is the sweeper's side of a round: it deletes the message as
// soon as its end has passed, or sees it gone.
func sweepUntilGone(a *app, msg uuid.UUID) error {
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		d, err := messages.DeleteExpired(context.Background(), a.db, msg)
		if err != nil {
			return err
		}
		if d != nil || a.count(`SELECT COUNT(*) FROM messages WHERE id = $1`, msg) == 0 {
			return nil
		}
		time.Sleep(2 * time.Millisecond)
	}
	return errors.New("the message did not end")
}

func TestTheEndOfALifetimeRacesWhatMembersDoToTheMessage(t *testing.T) {
	a := newTimedApp(t)
	f := &timedRaceWorld{a: a, owner: a.seedAdmin()}
	f.bob = a.register(f.owner, "bob")
	f.ch, f.other = a.createChannel(f.owner, "general", "text"), a.createChannel(f.owner, "other", "text")
	wantStatus(t, "auto-delete", f.owner.put("/api/admin/channels/"+f.ch.String()+"/auto-delete", map[string]any{"hours": 1}), http.StatusOK)
	author := a.register(f.owner, "alice")
	for _, race := range timedRaces {
		t.Run(race.name, func(t *testing.T) {
			var msg uuid.UUID
			who := author
			racetest.Sweep(t, racetest.Rounds(4, 60), 2,
				func(round int) {
					if race.fresh {
						who = f.member(fmt.Sprintf("%s-author%d", race.name[:3], round))
					}
					if race.freshBob {
						f.bob = f.member(fmt.Sprintf("%s-member%d", race.name[:3], round))
					}
					// Two hours old for the auto-delete, ending 30 ms from now.
					if err := a.db.QueryRow(context.Background(), `
						INSERT INTO messages (channel_id, user_id, content, created_at, expires_at)
						VALUES ($1, $2, 'ending soon', now() - interval '2 hours', now() + interval '30 milliseconds') RETURNING id`,
						f.ch, who.user.ID).Scan(&msg); err != nil {
						t.Fatal(err)
					}
				},
				func(round, i int) error {
					if i == 0 {
						return sweepUntilGone(a, msg)
					}
					time.Sleep(25 * time.Millisecond)
					return statusErr(race.act(f, msg, who), race.ok...)
				},
				func(round int, r racetest.Round) string {
					for i, err := range r.Errs {
						if err != nil {
							return fmt.Sprintf("round %d caller %d: %v", round, i, err)
						}
					}
					if n := a.count(`SELECT COUNT(*) FROM messages WHERE id = $1`, msg); n != 0 {
						return fmt.Sprintf("round %d: the message outlived its end", round)
					}
					if race.left != "" {
						if n := a.count(race.left, msg); n != 0 {
							return fmt.Sprintf("round %d: %d rows the message should have taken along", round, n)
						}
					}
					return ""
				}).FailOnViolation(t)
		})
	}
}
