//go:build integration

package server

import (
	"context"
	"fmt"
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

// Lane 10 of the defect hunt, part two. A write by the member that is in
// flight while the member erases the account: the request was authenticated
// before the erasure committed, and its statements wait for the erasure's lock
// on the users row (FOR UPDATE, which every foreign key check to users and
// every update of that row has to wait for). The erasure is held open by the
// test right before it anonymises the account, after every erase step ran, so
// the write reaches the lock and waits there (a real interleaving through the
// real routes, forced with pgx's tracing hook). Once the erasure commits, the
// write must not leave the member's words or rows behind.

// erasureHold runs fn once, on the erasure's connection, before the statement
// that anonymises the account: the transaction holds the users row then.
type erasureHold struct {
	once sync.Once
	fn   func()
	hit  bool
}

const anonymiseMark = "display_name = 'Deleted member'"

func (h *erasureHold) TraceQueryStart(ctx context.Context, _ *pgx.Conn, d pgx.TraceQueryStartData) context.Context {
	if strings.Contains(strings.Join(strings.Fields(d.SQL), " "), anonymiseMark) {
		h.once.Do(func() { h.hit = true; h.fn() })
	}
	return ctx
}

func (*erasureHold) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

// heldErasureApp is newApp whose pool carries the hold.
func heldErasureApp(t *testing.T, hold *erasureHold) *app {
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
		if !hold.hit {
			t.Errorf("the hold never saw the statement with %q", anonymiseMark)
		}
	})
	return a
}

// inFlightWorld is the canary world plus an event to comment on.
type inFlightWorld struct {
	canaryWorld
	event uuid.UUID
	group uuid.UUID // a group conversation bob started, so the erasure does not lock its row
}

// inFlightSite is one write of the member. With rows set, the leak is the
// count of that query for the member's id (a write without words); otherwise
// it is every row that still holds the word.
type inFlightSite struct {
	name  string
	write func(f *inFlightWorld, word string) response
	rows  string
}

func (f *inFlightWorld) messages(content string, extra map[string]any) response {
	body := map[string]any{"content": content}
	for k, v := range extra {
		body[k] = v
	}
	return f.alice.post("/api/channels/"+f.ch.String()+"/messages", body)
}

// inFlightSites are the canary sites (all but the forward, which is a send by
// alice and a forward by bob) and the writes without words.
func inFlightSites() []inFlightSite {
	var out []inFlightSite
	for _, s := range canarySites {
		if s.name == "forwarded" {
			continue
		}
		write := s.write
		out = append(out, inFlightSite{name: s.name, write: func(f *inFlightWorld, w string) response { return write(&f.canaryWorld, w) }})
	}
	return append(out,
		inFlightSite{name: "unsent file", write: func(f *inFlightWorld, w string) response {
			return f.alice.upload("/api/channels/"+f.ch.String()+"/attachments", "file", w+".png", "image/png", pngBytes, nil)
		}},
		inFlightSite{name: "display name", write: func(f *inFlightWorld, w string) response {
			return f.alice.put("/api/users/me/profile", map[string]string{"display_name": w})
		}},
		inFlightSite{name: "group message", write: func(f *inFlightWorld, w string) response { return f.alice.dmPost(f.group, w) }},
		inFlightSite{name: "event comment", write: func(f *inFlightWorld, w string) response {
			return f.alice.post("/api/events/"+f.event.String()+"/comments", map[string]string{"content": w})
		}},
		inFlightSite{name: "retry id", rows: `SELECT COUNT(*) FROM message_nonces WHERE user_id = $1`, write: func(f *inFlightWorld, _ string) response {
			return f.messages("with a retry id", map[string]any{"nonce": uuid.New()})
		}},
		inFlightSite{name: "upload booking", rows: `SELECT COUNT(*) FROM upload_usage WHERE user_id = $1`, write: func(f *inFlightWorld, _ string) response {
			return f.alice.upload("/api/channels/"+f.ch.String()+"/attachments", "file", "booked.png", "image/png", pngBytes, nil)
		}},
		inFlightSite{name: "reaction", rows: `SELECT COUNT(*) FROM message_reactions WHERE user_id = $1`, write: func(f *inFlightWorld, _ string) response {
			return f.alice.post("/api/messages/"+f.bobMsg.String()+"/reactions", map[string]string{"emoji": "🔥"})
		}},
		inFlightSite{name: "rsvp", rows: `SELECT COUNT(*) FROM event_rsvps WHERE user_id = $1`, write: func(f *inFlightWorld, _ string) response {
			return f.alice.put("/api/events/"+f.event.String()+"/rsvp", map[string]string{"response": "going"})
		}},
	)
}

// inFlightLeaks are the sites whose write outlives the erasure today: known
// failures of H-L10-05 (inverse assertion: the site must still leak, and the
// test turns red the day it does not, so the entry goes with the fix). No
// statement of these writes reads the account again after its wait: the
// insert's foreign key check (or the update of the users row) waits for the
// erasure, then finds the anonymised row and goes through.
//
// The one site that holds today is "direct message", for a reason of its
// own: alice started that conversation, so the erasure's
// `UPDATE channels SET dm_created_by = NULL` locks its row; the send's
// `SELECT ... FROM channels WHERE id = $1 FOR UPDATE` (loadDMState) waits for
// it, and the member list read next, in a new snapshot, no longer holds alice
// (404). In a conversation someone else started ("group message") nothing
// locks that row and the message stays.
var inFlightLeaks = map[string]bool{
	"message": true, "edited away": true, "bio": true, "status": true, "display name": true,
	"file name": true, "unsent file": true, "saved note": true, "report note": true, "scheduled": true,
	"group message": true, "event comment": true, "retry id": true, "upload booking": true,
	"reaction": true, "rsvp": true,
}

// inFlightOutcome is what one held erasure saw.
type inFlightOutcome struct {
	status int
	waited time.Duration
	left   string
}

func runInFlightSite(t *testing.T, s inFlightSite) inFlightOutcome {
	hold := &erasureHold{}
	a := heldErasureApp(t, hold)
	f := &inFlightWorld{}
	f.a, f.admin = a, a.seedAdmin()
	f.alice, f.bob = a.register(f.admin, "alice"), a.register(f.admin, "bob")
	f.ch = a.createChannel(f.admin, "general", "text")
	f.bobMsg = a.send(f.bob, f.ch, "bob's words")
	f.aliceMsg = a.send(f.alice, f.ch, "to be edited")
	f.dm = f.alice.mustDM(t, f.bob).ID
	f.group = f.bob.mustDM(t, f.alice, f.admin).ID
	f.event = a.coverEventAt(f.admin, time.Now().Add(48*time.Hour), nil).ID
	word := "inflight" + strings.ReplaceAll(s.name, " ", "") + "x7q"

	var out inFlightOutcome
	done := make(chan struct{})
	hold.fn = func() {
		go func() {
			defer close(done)
			began := time.Now()
			out.status = s.write(f, word).status
			out.waited = time.Since(began)
		}()
		time.Sleep(500 * time.Millisecond) // the write reaches the users row's lock and waits there
	}
	if res := a.deleteAccount(f.alice, memberPassword, ""); res.status != http.StatusNoContent {
		t.Fatalf("delete: %d %s", res.status, res.body)
	}
	<-done
	if out.waited < 400*time.Millisecond {
		t.Fatalf("the write answered %d after %v: it did not wait for the erasure, the interleaving did not happen", out.status, out.waited)
	}
	if s.rows != "" {
		if n := a.countRows(s.rows, f.alice.user.ID); n > 0 {
			out.left = fmt.Sprintf("%s: %d rows", strings.Fields(s.rows)[3], n)
		}
	} else if hits := a.everyRowMentioning(word); len(hits) > 0 {
		out.left = fmt.Sprint(hits)
	}
	return out
}

func TestAWriteInFlightDuringTheErasureLeavesNothingOfThePerson(t *testing.T) {
	for _, s := range inFlightSites() {
		t.Run(s.name, func(t *testing.T) {
			out := runInFlightSite(t, s)
			t.Logf("%s: the write answered %d after %v; left behind: %q", s.name, out.status, out.waited, out.left)
			switch {
			case inFlightLeaks[s.name] && out.left == "":
				t.Errorf("known failure H-L10-05 no longer reproduces for %s (answer %d): drop it from inFlightLeaks", s.name, out.status)
			case !inFlightLeaks[s.name] && out.left != "":
				t.Errorf("%s: a write that waited for the erasure left the erased person's data behind: %s", s.name, out.left)
			}
		})
	}
}
