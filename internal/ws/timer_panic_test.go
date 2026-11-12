package ws

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/metrics"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/perm/permtest"
)

// A panic in the timer that ends one member's seat after the grace time is
// that member's alone: the process carries on, the seat is gone, no grace and
// no lock stays behind, the others in the Talk still hear the leave, and the
// next member's own grace timer still ends their seat. A process exit
// (safego.Crash) would end this test binary.
func TestPanicInOneMembersGraceTimerLeavesTheHubConsistent(t *testing.T) {
	h := NewHub(nil, nil, nil, nil, openPerms())
	h.VoiceGrace = 20 * time.Millisecond
	t.Cleanup(h.Close)
	ch := voiceCh()
	bad := testClient(h, auth.User{ID: uuid.New(), Username: "bad"})
	good := testClient(h, auth.User{ID: uuid.New(), Username: "good"})
	other := testClient(h, auth.User{ID: uuid.New(), Username: "other"})
	h.Presence = func(user, _ uuid.UUID, joined bool) {
		if !joined && user == bad.User.ID {
			panic("presence hook bug")
		}
	}
	for _, c := range []*Client{bad, good, other} {
		h.joinVoice(c, ch)
	}
	drainTypes(t, other)
	before := metrics.GoroutinePanics.With("ws_voice_grace").Load()

	h.unregister(bad)
	waitCounted(t, "ws_voice_grace", before+1)
	waitSeat(t, h, ch.ID, bad.User.ID, false)
	assertHubUsable(t, h)
	if graces := graceCount(h); graces != 0 {
		t.Fatalf("%d graces left after the panic", graces)
	}
	if !inRoom(h, ch.ID, good.User.ID) || !inRoom(h, ch.ID, other.User.ID) {
		t.Fatal("the panic removed a member who did not leave")
	}

	// Another member's timer is unaffected by the earlier panic.
	h.unregister(good)
	waitSeat(t, h, ch.ID, good.User.ID, false)
	if !inRoom(h, ch.ID, other.User.ID) {
		t.Fatal("the member still connected lost their seat")
	}
}

// The leave is still told to the others when the listener panicked after the
// seat was removed: the broadcast is deferred in endPresence.
func TestGraceTimerPanicStillTellsTheOthers(t *testing.T) {
	h := NewHub(nil, nil, nil, nil, openPerms())
	h.VoiceGrace = 20 * time.Millisecond
	t.Cleanup(h.Close)
	ch := voiceCh()
	bad := testClient(h, auth.User{ID: uuid.New(), Username: "bad"})
	other := testClient(h, auth.User{ID: uuid.New(), Username: "other"})
	h.Presence = func(user, _ uuid.UUID, joined bool) {
		if !joined && user == bad.User.ID {
			panic("presence hook bug")
		}
	}
	h.joinVoice(bad, ch)
	h.joinVoice(other, ch)
	drainTypes(t, other)
	before := metrics.GoroutinePanics.With("ws_voice_grace").Load()

	h.unregister(bad)
	waitCounted(t, "ws_voice_grace", before+1)
	waitSeat(t, h, ch.ID, bad.User.ID, false) // nobody keeps the seat the timer ended
	if !inRoom(h, ch.ID, other.User.ID) {
		t.Fatal("the member who did not leave lost their seat")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		types := drainTypes(t, other)
		if count(types, events.VoiceStateUpdate) > 0 { // everything earlier was drained
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the others never heard the leave, got %v", types)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// A panic while voice time is written (here: no database behind the hub) is
// abandoned: the seconds are put back, the entry's lock is free, and the
// entry is still there for the next flush. Other members' entries are
// untouched.
func TestPanicInVoiceTimeTimerKeepsTheSecondsAndTheLock(t *testing.T) {
	h := NewHub(nil, nil, nil, nil, openPerms())
	t.Cleanup(h.Close)
	bad, bystander := uuid.New(), uuid.New()
	ch := openChannel()
	h.vtimes.mu.Lock()
	h.vtimes.m = map[uuid.UUID]*pendingVoiceTime{
		bad:       {secs: 7, public: 7, channel: ch},
		bystander: {secs: 3, channel: ch},
	}
	h.vtimes.mu.Unlock()
	before := metrics.GoroutinePanics.With("ws_voice_time").Load()

	timer := h.armVoiceTimeFlush(bad, 10*time.Millisecond)
	t.Cleanup(func() { timer.Stop() })
	waitCounted(t, "ws_voice_time", before+1)

	waitFor(t, "the seconds put back", func() bool {
		if !h.vtimes.mu.TryLock() {
			return false
		}
		defer h.vtimes.mu.Unlock()
		p := h.vtimes.m[bad]
		return p != nil && p.secs == 7 && p.public == 7 && p.timer != nil
	})
	h.vtimes.mu.Lock()
	b := h.vtimes.m[bystander]
	h.vtimes.mu.Unlock()
	if b == nil || b.secs != 3 {
		t.Fatalf("another member's entry changed: %+v", b)
	}
	// Stop the retry the restore armed and drop the entries: the hub's Close
	// would flush them against the missing database.
	h.vtimes.mu.Lock()
	h.vtimes.m[bad].timer.Stop()
	clear(h.vtimes.m)
	h.vtimes.mu.Unlock()
}

// A panic part-way through enforcing a permission change must not leave a
// member with the seat the change took away. The change is committed and
// nothing else re-checks live connections, so the enforcement fails closed:
// the process lives, the caller (an HTTP handler) is not handed the panic, the
// hub lock is free, and every member the change concerned is disconnected and
// out of the Talk. A member it did not concern is untouched.
func TestPanicInPermissionEnforcementFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name      string
		ids       func(a, b uuid.UUID) []uuid.UUID
		bystander bool // whether the member the change does not concern keeps the seat
	}{
		{"named members", func(a, b uuid.UUID) []uuid.UUID { return []uuid.UUID{a, b} }, true},
		{"everybody", func(_, _ uuid.UUID) []uuid.UUID { return nil }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := permtest.NewFixture()
			svc := perm.NewServiceFrom(func(context.Context, time.Time) (*perm.Snapshot, error) { return f.Snapshot(), nil })
			h := NewHub(nil, nil, nil, nil, svc)
			t.Cleanup(h.Close)
			talk := &core.ChannelInfo{ID: f.Channel(uuid.Nil), Type: core.ChannelTypeVoice}
			a := testClient(h, auth.User{ID: uuid.New(), Username: "a"})
			b := testClient(h, auth.User{ID: uuid.New(), Username: "b"})
			keep := testClient(h, auth.User{ID: uuid.New(), Username: "keep"})
			for _, c := range []*Client{a, b, keep} {
				h.joinVoice(c, talk)
			}
			var first atomic.Bool
			h.Presence = func(_, _ uuid.UUID, joined bool) {
				if !joined && first.CompareAndSwap(false, true) {
					panic("enforcement bug")
				}
			}
			before := metrics.GoroutinePanics.With("ws_perm_enforce").Load()

			// The change takes Connect from everyone; the first removal panics.
			f.RoleOverwrite(true, talk.ID, f.EveryoneID(), 0, perm.Connect)
			svc.Invalidate()
			h.PermissionsChanged(tc.ids(a.User.ID, b.User.ID)) // must not panic

			if got := metrics.GoroutinePanics.With("ws_perm_enforce").Load(); got != before+1 {
				t.Fatalf("counted %d panics, want %d", got, before+1)
			}
			assertHubUsable(t, h)
			for _, c := range []*Client{a, b} {
				if inRoom(h, talk.ID, c.User.ID) {
					t.Fatalf("%s kept the seat the change took away", c.User.Username)
				}
				if !c.closed.Load() {
					t.Fatalf("%s kept a connection that was not re-checked", c.User.Username)
				}
			}
			if stays := inRoom(h, talk.ID, keep.User.ID); stays != tc.bystander {
				t.Fatalf("the unconcerned member's seat = %v, want %v", stays, tc.bystander)
			}
		})
	}
}

func graceCount(h *Hub) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.grace)
}

func assertHubUsable(t *testing.T, h *Hub) {
	t.Helper()
	if !h.mu.TryLock() {
		t.Fatal("the hub lock stayed held after the panic")
	}
	h.mu.Unlock()
}

func waitSeat(t *testing.T, h *Hub, ch, user uuid.UUID, want bool) {
	t.Helper()
	waitFor(t, "the seat to change", func() bool { return inRoom(h, ch, user) == want })
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); !ok(); time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}
