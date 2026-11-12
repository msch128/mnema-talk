package ws

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/events/wire"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/perm/permtest"
)

// cleanupWorld is a private Talk with two seated members, a and b, whose
// right to it is about to be taken away while the first enforcement pass
// panics, so the fail-closed clean-up (enforceFailClosed) has to take over.
type cleanupWorld struct {
	f    *permtest.Fixture
	svc  *perm.Service
	h    *Hub
	talk *core.ChannelInfo
	a, b *Client
}

func newCleanupWorld(t *testing.T) *cleanupWorld {
	t.Helper()
	f := permtest.NewFixture()
	svc := perm.NewServiceFrom(func(context.Context, time.Time) (*perm.Snapshot, error) { return f.Snapshot(), nil })
	h := NewHub(nil, nil, nil, nil, svc)
	t.Cleanup(h.Close)
	w := &cleanupWorld{f: f, svc: svc, h: h, talk: &core.ChannelInfo{ID: f.Channel(uuid.Nil), Type: core.ChannelTypeVoice}}
	w.a = testClient(h, auth.User{ID: uuid.New(), Username: "a"})
	w.b = testClient(h, auth.User{ID: uuid.New(), Username: "b"})
	for _, c := range []*Client{w.a, w.b} {
		h.joinVoice(c, w.talk)
		if !inRoom(h, w.talk.ID, c.User.ID) {
			t.Fatal("could not take a seat")
		}
	}
	// The first enforcement pass panics on its first removal.
	var first atomic.Bool
	h.Presence = func(_, _ uuid.UUID, joined bool) {
		if !joined && first.CompareAndSwap(false, true) {
			panic("enforcement bug")
		}
	}
	return w
}

// revoke hides the Talk from everyone and tells the hub.
func (w *cleanupWorld) revoke() {
	w.f.RoleOverwrite(true, w.talk.ID, w.f.EveryoneID(), 0, perm.ViewChannel)
	w.svc.Invalidate()
	w.h.PermissionsChanged([]uuid.UUID{w.a.User.ID, w.b.User.ID})
}

// receivesTalkEvents reports whether c is still sent an event of the Talk.
func receivesTalkEvents(t *testing.T, h *Hub, c *Client, talk uuid.UUID) bool {
	t.Helper()
	drainTypes(t, c)
	h.BroadcastChannel(talk, wire.VoiceLeave{Action: "leave", ChannelID: talk, UserID: uuid.New()})
	return len(drainTypes(t, c)) > 0
}

// AB-07: the clean-up's disconnect of one member panics (its done channel is
// closed already, so shutdown panics under the hub lock) and is abandoned. The
// member keeps the socket, but no right: its seat is ended by the seat pass and
// every event is filtered on the new state, so nothing of the Talk reaches it.
func TestAMemberWhoseCleanUpDisconnectPanicsKeepsTheSocketButNoRight(t *testing.T) {
	w := newCleanupWorld(t)
	broken := make(chan struct{})
	close(broken)
	w.a.done = broken

	w.revoke()

	for _, c := range []*Client{w.a, w.b} {
		if inRoom(w.h, w.talk.ID, c.User.ID) {
			t.Fatalf("%s kept the seat", c.User.Username)
		}
	}
	if !w.b.closed.Load() {
		t.Fatal("the other member was not disconnected")
	}
	if receivesTalkEvents(t, w.h, w.a, w.talk.ID) {
		t.Fatal("the member whose disconnect panicked still receives the Talk's events")
	}
	w.h.joinVoice(w.a, w.talk)
	if inRoom(w.h, w.talk.ID, w.a.User.ID) {
		t.Fatal("the member could take the seat again")
	}
}

// AB-08: the clean-up's seat removal of one member panics before the seat is
// gone, every time (a grace entry without a timer: takeGrace stops a nil
// timer; production always sets the timer, so this stands for any
// deterministic panic before the removal). The member still loses the right:
// the connection pass closed the socket first, so nothing of the Talk reaches
// it, and the other member's seat is ended. What stays is the listing, and
// nothing ends it later: no timer re-checks seats (the periodic check in
// lifecycle_pumps.go revalidates sessions only). This pins that state; a
// change that ends the listing makes the "still listed" check fail (an
// expected-to-fail check written the other way round, see the lane 4 report).
func TestAMemberWhoseCleanUpSeatRemovalPanicsLosesTheConnectionButStaysListed(t *testing.T) {
	w := newCleanupWorld(t)
	key := voiceKey{w.a.User.ID, w.talk.ID}
	w.h.mu.Lock()
	w.h.grace[key] = &graceLeave{channelID: w.talk.ID}
	w.h.mu.Unlock()
	t.Cleanup(func() { // registered after newCleanupWorld's h.Close, so it runs first
		w.h.mu.Lock()
		delete(w.h.grace, key)
		w.h.mu.Unlock()
	})

	w.revoke()

	if !w.a.closed.Load() {
		t.Fatal("the member whose seat removal panicked kept the connection")
	}
	if receivesTalkEvents(t, w.h, w.a, w.talk.ID) {
		t.Fatal("the member keeps receiving the Talk's events")
	}
	if inRoom(w.h, w.talk.ID, w.b.User.ID) {
		t.Fatal("the other member kept the seat")
	}
	time.Sleep(200 * time.Millisecond)
	if !inRoom(w.h, w.talk.ID, w.a.User.ID) {
		t.Fatal("the stale listing is gone now: something ends it; update this test and the lane 4 matrix")
	}
}

// A database outage that starts right after the change was stored: the hub
// cannot load the new state, so the member keeps the seat until the outage
// ends and the next retry runs (permRetry doubles from base up to a minute,
// internal/ws/perms.go:197-245). The seat ends within the outage plus one
// retry wait, and never before the outage is over.
func TestASeatOutlivesAnOutageByAtMostOneRetryWait(t *testing.T) {
	for _, outage := range []time.Duration{50 * time.Millisecond, 300 * time.Millisecond, 700 * time.Millisecond} {
		f := permtest.NewFixture()
		var down atomic.Bool
		svc := perm.NewServiceFrom(func(context.Context, time.Time) (*perm.Snapshot, error) {
			if down.Load() {
				return nil, context.DeadlineExceeded
			}
			return f.Snapshot(), nil
		})
		h := NewHub(nil, nil, nil, nil, svc)
		h.permRetry.base = 20 * time.Millisecond
		talk := &core.ChannelInfo{ID: f.Channel(uuid.Nil), Type: core.ChannelTypeVoice}
		m := testClient(h, auth.User{ID: uuid.New()})
		h.joinVoice(m, talk)

		f.RoleOverwrite(true, talk.ID, f.EveryoneID(), 0, perm.Connect)
		svc.Invalidate()
		down.Store(true)
		start := time.Now()
		h.PermissionsChanged([]uuid.UUID{m.User.ID})
		time.AfterFunc(outage, func() { down.Store(false) })
		for inRoom(h, talk.ID, m.User.ID) && time.Since(start) < 5*time.Second {
			time.Sleep(time.Millisecond)
		}
		took := time.Since(start)
		// The waits are 20, 40, 80, ... ms: the first retry after the outage
		// comes at most one wait (under twice the outage, plus base) later.
		if took < outage || took > 2*outage+100*time.Millisecond {
			t.Errorf("outage %v: the seat ended after %v", outage, took)
		}
		t.Logf("outage %v: seat held %v", outage, took.Round(time.Millisecond))
		h.Close()
	}
}
