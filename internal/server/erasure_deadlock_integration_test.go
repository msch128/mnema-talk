//go:build integration

package server

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/msch128/mnema-talk/internal/db"
)

// H-L10-06. The erasure locks the member's users row first and their messages,
// memberships and conversations later, step by step. A write of the same
// member's (or about them) that locked one of those rows first and then waits
// for the users row, or for a row the erasure holds, closes a cycle when the
// erasure reaches that row. PostgreSQL aborts the transaction whose deadlock
// check runs while the cycle exists: when the write has waited longer than
// deadlock_timeout (1 s) by then, that is the erasure. The test holds the
// erasure right before the statement that closes the cycle, starts the write
// during the hold, and lets the erasure go on after holdFor.

// stepHold runs fn on the erasure's connection before a statement whose
// text contains mark: once, or as often as times says (once per attempt).
type stepHold struct {
	mu    sync.Mutex
	mark  string
	times int
	fn    func()
	hits  int
}

func (h *stepHold) TraceQueryStart(ctx context.Context, _ *pgx.Conn, d pgx.TraceQueryStartData) context.Context {
	if strings.Contains(strings.Join(strings.Fields(d.SQL), " "), h.mark) {
		h.mu.Lock()
		hold := h.hits < max(h.times, 1)
		if hold {
			h.hits++
		}
		h.mu.Unlock()
		if hold {
			h.fn()
		}
	}
	return ctx
}

func (*stepHold) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

const (
	messagesStepMark  = "UPDATE messages SET content = '', title = NULL"
	dmCreatedByMark   = "UPDATE channels SET dm_created_by = NULL"
	talkLeaderMark    = "UPDATE channels SET leader_id = NULL"
	longerThanTimeout = 2500 * time.Millisecond // the write waits past deadlock_timeout
)

// stepHeldApp is newApp whose pool carries the hold.
func stepHeldApp(t *testing.T, hold *stepHold) *app {
	t.Helper()
	a := newAppWithDeps(t, false, nil, func(d *Deps) {
		config := d.DB.Config().Copy()
		config.ConnConfig.Tracer = hold
		config.MinConns = 0
		p, err := pgxpool.NewWithConfig(context.Background(), config)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(p.Close)
		d.DB = &db.Pool{Pool: p}
	})
	t.Cleanup(func() {
		if hold.hits == 0 {
			t.Errorf("the hold never saw the statement with %q", hold.mark)
		}
	})
	return a
}

// eraseDuring erases member's account while write runs: write starts each
// time the erasure reaches the held statement, and the erasure goes on after
// holdFor. wrote is the last write's answer.
func eraseDuring(a *app, hold *stepHold, member *client, holdFor time.Duration, write func() response) (erasure, wrote response) {
	var wg sync.WaitGroup
	var mu sync.Mutex
	hold.fn = func() {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res := write()
			mu.Lock()
			wrote = res
			mu.Unlock()
		}()
		time.Sleep(holdFor)
	}
	erasure = a.deleteAccount(member, memberPassword, "")
	wg.Wait()
	return erasure, wrote
}

// wantErased checks that the erasure answered 204 and did its work: the
// account is a tombstone and its messages are empty and unpinned.
func wantErased(t *testing.T, a *app, member *client, erasure response) {
	t.Helper()
	if erasure.status != http.StatusNoContent {
		t.Fatalf("the erasure answered %d %s: a deadlock it lost must be retried", erasure.status, erasure.body)
	}
	id := member.user.ID
	if n := a.countRows(`SELECT COUNT(*) FROM users WHERE id = $1 AND deleted_at IS NOT NULL`, id); n != 1 {
		t.Fatal("the account is not erased")
	}
	if n := a.countRows(`SELECT COUNT(*) FROM messages WHERE user_id = $1 AND (content <> '' OR is_pinned)`, id); n != 0 {
		t.Fatalf("%d of the member's messages kept their text or pin", n)
	}
}

func TestErasureSurvivesADeadlockWithTheMembersOwnEdit(t *testing.T) {
	for _, holdFor := range []time.Duration{300 * time.Millisecond, longerThanTimeout} {
		t.Run(holdFor.String(), func(t *testing.T) {
			hold := &stepHold{mark: messagesStepMark}
			a := stepHeldApp(t, hold)
			admin := a.seedAdmin()
			alice := a.register(admin, "alice")
			ch := a.createChannel(admin, "general", "text")
			msg := a.send(alice, ch, "to be edited")
			erasure, edit := eraseDuring(a, hold, alice, holdFor, func() response {
				return alice.put("/api/channels/"+ch.String()+"/messages/"+msg.String(), map[string]any{"content": "edited from a second device"})
			})
			t.Logf("held %v: erasure %d %s; edit %d", holdFor, erasure.status, erasure.body, edit.status)
			// Held 300 ms, the edit's own deadlock check runs first and the edit
			// is the victim; held 2.5 s, the erasure's check runs first.
			wantErased(t, a, alice, erasure)
		})
	}
}

// The retry is bounded: an erasure that loses the deadlock on every attempt
// (account.eraseAttempts, 3) answers 503 UNAVAILABLE with the sentence the
// web app shows as "The server is busy. Try again in a moment.", and nothing
// was erased: the member is still signed in.
func TestErasureThatKeepsLosingDeadlocksAsksToTryAgain(t *testing.T) {
	hold := &stepHold{mark: messagesStepMark, times: 3}
	a := stepHeldApp(t, hold)
	admin := a.seedAdmin()
	alice := a.register(admin, "alice")
	ch := a.createChannel(admin, "general", "text")
	msg := a.send(alice, ch, "to be edited")
	erasure, _ := eraseDuring(a, hold, alice, longerThanTimeout, func() response {
		return alice.put("/api/channels/"+ch.String()+"/messages/"+msg.String(), map[string]any{"content": "edited again"})
	})
	if erasure.status != http.StatusServiceUnavailable {
		t.Fatalf("the erasure answered %d %s, want 503", erasure.status, erasure.body)
	}
	if e := erasure.errorBody(t); e.Error.Code != "UNAVAILABLE" || e.Error.Message != "the server is busy, try again shortly" {
		t.Fatalf("the erasure answered %s", erasure.body)
	}
	if hold.hits != 3 {
		t.Fatalf("the erasure ran %d times, want 3", hold.hits)
	}
	if res := alice.get("/api/auth/me"); res.status != http.StatusOK {
		t.Fatalf("after the refused erasure the member's session answers %d", res.status)
	}
	if n := a.countRows(`SELECT COUNT(*) FROM users WHERE id = $1 AND deleted_at IS NULL`, alice.user.ID); n != 1 {
		t.Fatal("a refused erasure changed the account")
	}
}

// groupOf is a group conversation alice started with bob and the Owner, so
// the erasure's `UPDATE channels SET dm_created_by = NULL` locks its row.
func groupOf(t *testing.T, alice, bob, admin *client) uuid.UUID {
	t.Helper()
	return alice.mustDM(t, bob, admin).ID
}

func TestErasureSurvivesADeadlockWithTheMembersOwnLeave(t *testing.T) {
	hold := &stepHold{mark: dmCreatedByMark}
	a := stepHeldApp(t, hold)
	admin := a.seedAdmin()
	alice, bob := a.register(admin, "alice"), a.register(admin, "bob")
	group := groupOf(t, alice, bob, admin)
	// The leave locks the conversation, then waits for alice's membership row,
	// which the erasure deleted; the erasure then wants the conversation.
	erasure, leave := eraseDuring(a, hold, alice, longerThanTimeout, func() response {
		return alice.delete("/api/dms/" + group.String() + "/members/@me")
	})
	t.Logf("erasure %d %s; leave %d %s", erasure.status, erasure.body, leave.status, leave.body)
	wantErased(t, a, alice, erasure)
}

// The pin of the member's reply in a group conversation they started (the
// review's note) cannot be made through the routes: nobody holds Manage
// messages in a conversation, the Owner neither (403). The same cycle forms in
// a Talk the member leads, where the Owner may pin: the pin locks the Talk,
// then waits for the member's message, which the erasure's messages step
// emptied; the erasure then wants the Talk (`UPDATE channels SET leader_id`).
func TestErasureSurvivesADeadlockWithAPinInATalkTheMemberLeads(t *testing.T) {
	hold := &stepHold{mark: talkLeaderMark}
	a := stepHeldApp(t, hold)
	admin := a.seedAdmin()
	alice := a.register(admin, "alice")
	var talk, msg uuid.UUID
	ctx := context.Background()
	if err := a.db.QueryRow(ctx, `INSERT INTO channels (name, type, is_temp, leader_id) VALUES ('alice talk', 'voice', TRUE, $1) RETURNING id`, alice.user.ID).Scan(&talk); err != nil {
		t.Fatal(err)
	}
	if err := a.db.QueryRow(ctx, `INSERT INTO messages (channel_id, user_id, content) VALUES ($1, $2, 'in the talk') RETURNING id`, talk, alice.user.ID).Scan(&msg); err != nil {
		t.Fatal(err)
	}
	erasure, pin := eraseDuring(a, hold, alice, longerThanTimeout, func() response {
		return admin.put("/api/channels/"+talk.String()+"/messages/"+msg.String()+"/pin", nil)
	})
	t.Logf("erasure %d %s; pin %d %s", erasure.status, erasure.body, pin.status, pin.body)
	if pin.status != http.StatusOK {
		t.Fatalf("the pin answered %d: the interleaving did not happen", pin.status)
	}
	wantErased(t, a, alice, erasure)
}
