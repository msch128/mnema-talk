//go:build integration

package server

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// revocationWorld is one member (the victim) who holds everything a right can
// stand behind before it is taken away: an open socket, a seat in a private
// Talk, a pending scheduled message, a saved message, a mention, a followed
// thread and an attachment it has fetched, all in channels only the Staff role
// may view. The Owner is the peer who keeps posting after the revocation.
type revocationWorld struct {
	accessFixture
	victim   *client
	text     uuid.UUID // Staff-only text channel
	talk     uuid.UUID // Staff-only Talk
	victimWS *wsConn
	ownerWS  *wsConn
	fileURL  string
	root     uuid.UUID // a thread the victim follows
}

// Canary words: each sits in one object only, so a body that contains one
// names the surface that leaked it.
const (
	canaryMention   = "mention-canary-7f3a"
	canaryThread    = "thread-canary-91c2"
	canaryScheduled = "scheduled-canary-44d0"
	canaryLive      = "live-canary-"
)

// newRevocationWorld builds the world; prepare (optional) runs before any
// socket is opened, for a row that needs more state on the account.
func newRevocationWorld(t *testing.T, prepare func(*revocationWorld)) *revocationWorld {
	t.Helper()
	f := newAccessFixture(t, true)
	w := &revocationWorld{accessFixture: f, victim: f.insider}
	f.a.router.Hub.VoiceGrace = testGrace
	w.text = f.privateChannel(t, "staff-room", "text")
	w.talk = f.privateChannel(t, "staff-talk", "voice")
	if prepare != nil {
		prepare(w)
	}
	w.ownerWS = mustDial(t, f.owner)
	w.victimWS = mustDial(t, w.victim)
	w.seat(t)
	w.objects(t)
	return w
}

// seat puts the victim into the private Talk and waits until the hub lists it.
func (w *revocationWorld) seat(t *testing.T) {
	t.Helper()
	w.victimWS.send("voice_join", map[string]any{"channel_id": w.talk})
	if _, ok := w.ownerWS.expect("voice_state_update", soon); !ok || !w.seated() {
		t.Fatal("the victim could not take a seat in the private Talk")
	}
}

func (w *revocationWorld) seated() bool {
	return slices.Contains(w.a.router.Hub.VoiceMembers(w.talk), w.victim.user.ID)
}

// objects creates what the later surfaces hold, and checks each is reachable
// before anything is taken away (so a "stops" later is not a broken probe).
func (w *revocationWorld) objects(t *testing.T) {
	t.Helper()
	mention := postMessage(t, w.owner, w.text, map[string]any{"content": "@insider " + canaryMention})
	wantStatus(t, "save", w.victim.put("/api/me/saved/"+mention.String(), nil), http.StatusOK)
	w.root = postMessage(t, w.owner, w.text, map[string]any{"content": canaryThread})
	postMessage(t, w.owner, w.text, map[string]any{"content": "a reply", "parent_id": w.root})
	wantStatus(t, "follow", w.victim.put("/api/threads/"+w.root.String()+"/follow", nil), http.StatusOK)
	file := w.a.stage(w.owner, w.text, "plan.png")
	wantStatus(t, "send file", sendWith(w.owner, w.text, "the plan", file.ID), http.StatusCreated)
	w.fileURL = file.URL
	res := w.victim.post("/api/channels/"+w.text.String()+"/scheduled", map[string]any{"content": canaryScheduled, "send_at": inFuture(time.Hour)})
	wantStatus(t, "schedule", res, http.StatusCreated)
	for name, ok := range map[string]bool{
		"saved":      strings.Contains(string(w.victim.get("/api/me/saved").body), canaryMention),
		"mentions":   strings.Contains(string(w.victim.get("/api/mentions").body), canaryMention),
		"following":  strings.Contains(string(w.victim.get("/api/threads/following").body), w.root.String()),
		"attachment": w.victim.get(w.fileURL).status == http.StatusOK,
	} {
		if !ok {
			t.Fatalf("before the revocation the victim must reach %s", name)
		}
	}
	w.victimWS.drain()
	w.ownerWS.drain()
}

// drain empties what the socket has received so far.
func (c *wsConn) drain() {
	for {
		select {
		case _, ok := <-c.events:
			if !ok {
				return
			}
		default:
			return
		}
	}
}

// closedWithin reports whether the socket was closed by the server within d,
// and how long that took. Events read meanwhile are dropped.
func (c *wsConn) closedWithin(d time.Duration) (bool, time.Duration) {
	start := time.Now()
	deadline := time.After(d)
	for {
		select {
		case _, ok := <-c.events:
			if !ok {
				return true, time.Since(start)
			}
		case <-deadline:
			return false, d
		}
	}
}

// fireScheduled makes the victim's pending message due and runs the sweep the
// background ticker would run, then reports whether the message was posted.
func (w *revocationWorld) fireScheduled(t *testing.T) bool {
	t.Helper()
	ctx := context.Background()
	if _, err := w.a.db.Exec(ctx, `UPDATE scheduled_messages SET send_at = NOW() - INTERVAL '1 second' WHERE state = 'pending'`); err != nil {
		t.Fatal(err)
	}
	w.a.router.Later.Sweep(ctx)
	var n int
	if err := w.a.db.QueryRow(ctx, `SELECT COUNT(*) FROM messages WHERE content = $1`, canaryScheduled).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n > 0
}

// naNoSession marks a REST cell whose probe was refused because the victim's
// session ended: the surface is unreachable, which counts as stopped.
const naNoSession = "(session ended)"

// measure probes every cell right after the revocation answered. Socket and
// seat are polled up to their bounds; everything else is one probe.
func (w *revocationWorld) measure(t *testing.T) map[string]cellResult {
	t.Helper()
	start := time.Now()
	got := map[string]cellResult{}
	seat := make(chan cellResult, 1)
	go func() { seat <- w.probeSeat(start) }() // timed on its own clock, not after the other probes
	got[cellEvents] = w.probeEvents(t, start)
	closed, _ := w.victimWS.closedWithin(2*time.Second - time.Since(start))
	got[cellSocket] = cellResult{stopped: closed, took: time.Since(start)}
	got[cellSeat] = <-seat
	for cell, path := range map[string]string{cellSaved: "/api/me/saved", cellMentions: "/api/mentions", cellFollowing: "/api/threads/following"} {
		got[cell] = w.probeList(start, path, cell)
	}
	got[cellAttachment] = w.probeAttachment(start)
	posted := w.fireScheduled(t)
	got[cellScheduled] = cellResult{stopped: !posted, took: time.Since(start)}
	return got
}

// probeEvents posts a canary in the private channel (as the Owner, or not at
// all when the channel is gone) and reports whether the victim's socket
// delivered it.
func (w *revocationWorld) probeEvents(t *testing.T, start time.Time) cellResult {
	t.Helper()
	word := canaryLive + uuid.NewString()[:8]
	res := w.owner.post("/api/channels/"+w.text.String()+"/messages", map[string]any{"content": word})
	if res.status != http.StatusCreated {
		return cellResult{stopped: true, took: time.Since(start), note: fmt.Sprintf("(channel answers %d)", res.status)}
	}
	deadline := time.After(time.Second)
	for {
		select {
		case ev, ok := <-w.victimWS.events:
			if !ok {
				return cellResult{stopped: true, took: time.Since(start), note: "(socket closed)"}
			}
			if ev.Type == "message_create" && strings.Contains(string(ev.Payload), word) {
				return cellResult{stopped: false, took: time.Since(start), note: "(canary delivered)"}
			}
		case <-deadline:
			return cellResult{stopped: true, took: time.Since(start)}
		}
	}
}

// probeSeat polls the hub until the victim's seat is gone, up to the grace
// period plus a second.
func (w *revocationWorld) probeSeat(start time.Time) cellResult {
	deadline := time.Now().Add(testGrace + time.Second)
	for w.seated() {
		if time.Now().After(deadline) {
			return cellResult{stopped: false, took: time.Since(start)}
		}
		time.Sleep(10 * time.Millisecond)
	}
	return cellResult{stopped: true, took: time.Since(start)}
}

func (w *revocationWorld) probeList(start time.Time, path, cell string) cellResult {
	res := w.victim.get(path)
	if res.status == http.StatusUnauthorized {
		return cellResult{stopped: true, took: time.Since(start), note: naNoSession}
	}
	needle := canaryMention
	if cell == cellFollowing {
		needle = w.root.String() // the list holds thread ids only
	}
	if strings.Contains(string(res.body), needle) {
		return cellResult{stopped: false, took: time.Since(start), note: "(canary listed)"}
	}
	return cellResult{stopped: true, took: time.Since(start), note: fmt.Sprintf("(%d)", res.status)}
}

func (w *revocationWorld) probeAttachment(start time.Time) cellResult {
	res := w.victim.get(w.fileURL)
	switch res.status {
	case http.StatusUnauthorized:
		return cellResult{stopped: true, took: time.Since(start), note: naNoSession}
	case http.StatusOK:
		return cellResult{stopped: false, took: time.Since(start), note: "(200)"}
	}
	return cellResult{stopped: true, took: time.Since(start), note: fmt.Sprintf("(%d)", res.status)}
}
