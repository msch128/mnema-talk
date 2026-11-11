package ws

import (
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/events"
)

// A member's second connection that is in the middle of joining a Talk keeps
// the seat it just took when the member's first connection panics and is
// cleaned up: the clean-up gives up only seats nobody stands for.
func TestSeatOfAConnectionStillJoiningSurvivesTheCleanUpOfAnother(t *testing.T) {
	h := NewHub(nil, nil, nil, nil, openPerms())
	t.Cleanup(h.Close)
	user := auth.User{ID: uuid.New(), Username: "twotabs"}
	broken := testClient(h, user)
	joining := testClient(h, user)
	ch := voiceCh()

	if _, ok := h.takeSeat(joining, ch); !ok {
		t.Fatal("no seat")
	}
	broken.abandon()

	if !inRoom(h, ch.ID, user.ID) {
		t.Fatal("the seat of the connection that was joining was given up")
	}
	if left := sentEvents(t, joining, events.VoiceStateUpdate); len(left) != 0 {
		t.Fatalf("the member was announced as leaving: %+v", left)
	}
}

// A resume is silent because the member is still listed. When the seat was
// given up between the resume's first step and its seat (the clean-up of a
// panicking connection of the same member), the others heard "leave": the
// resume then is a join like any other, not a silent one.
func TestSeatGivenUpDuringAResumeIsTakenAsANewJoin(t *testing.T) {
	h := NewHub(nil, nil, nil, nil, openPerms())
	t.Cleanup(h.Close)
	user := auth.User{ID: uuid.New(), Username: "resuming"}
	ch := voiceCh()
	gone := testClient(h, user)
	h.joinVoice(gone, ch)
	h.unregister(gone) // the socket dropped: the seat lingers for the grace time
	broken := testClient(h, user)
	resuming := testClient(h, user)

	rejoin, _ := h.leaveOtherRooms(resuming, ch.ID)
	if !rejoin {
		t.Fatal("the lingering seat was not found")
	}
	broken.abandon()
	if inRoom(h, ch.ID, user.ID) {
		t.Fatal("the seat nobody stood for stayed listed")
	}
	seat, ok := h.takeSeat(resuming, ch)

	if !ok || seat.rejoin {
		t.Fatalf("seat taken=%t, silent rejoin=%t; want a seat that is announced", ok, seat.rejoin)
	}
}

// Once the hub is closed every stay is settled and the audit log is closing
// with the server: a connection that panics during shutdown reports no leave.
func TestPanickingConnectionOfAClosedHubReportsNoLeave(t *testing.T) {
	h := NewHub(nil, nil, nil, nil, openPerms())
	leaves := 0
	h.Presence = func(_, _ uuid.UUID, joined bool) {
		if !joined {
			leaves++
		}
	}
	c := testClient(h, auth.User{ID: uuid.New(), Username: "alice"})
	h.joinVoice(c, voiceCh())

	h.Close()
	c.abandon()

	if leaves != 0 {
		t.Fatalf("%d leaves reported after the hub was closed", leaves)
	}
}

// Two tabs of one member join the same Talk at the same moment. The later
// one wins and the earlier one is told; they must not evict each other, which
// would leave the member listed with no connection in the Talk.
func TestTwoTabsJoiningAtOnceLeaveTheLaterOneSeated(t *testing.T) {
	h := NewHub(nil, nil, newSeatTestSFU(t), nil, openPerms())
	ch := voiceCh()
	user := auth.User{ID: uuid.New(), Username: "twotabs"}
	first := testClient(h, user)
	second := testClient(h, user)
	defer h.unregister(first)
	defer h.unregister(second)
	// While second has its seat but no media peer yet, first joins completely.
	h.afterSeat = func() {
		h.afterSeat = nil
		h.joinVoice(first, ch)
	}

	h.joinVoice(second, ch)

	if cur := second.currentVoice(); cur == nil || *cur != ch.ID || second.peer() == nil {
		t.Fatal("the tab that finished its join last is not in the Talk")
	}
	if kicked := sentEvents(t, second, events.VoiceKicked); len(kicked) != 0 {
		t.Fatalf("the later tab was evicted: %+v", kicked)
	}
	if first.currentVoice() != nil {
		t.Fatal("the earlier tab still believes it owns the media")
	}
	if kicked := sentEvents(t, first, events.VoiceKicked); len(kicked) != 1 || kicked[0]["reason"] != kickJoinedElsewhere {
		t.Fatalf("voice_kicked to the earlier tab = %+v", kicked)
	}
	if !inRoom(h, ch.ID, user.ID) {
		t.Fatal("the member left the Talk")
	}
}

// A join that panics after the SFU gave it a media peer (here: while it tells
// the member's other tab) leaves no peer behind that the hub does not know.
func TestPanicAfterTheMediaJoinRemovesThePeer(t *testing.T) {
	media := newSeatTestSFU(t)
	h := NewHub(nil, nil, media, nil, openPerms())
	ch := voiceCh()
	user := auth.User{ID: uuid.New(), Username: "twotabs"}
	first := testClient(h, user)
	second := testClient(h, user)
	h.joinVoice(first, ch)
	close(first.send) // telling first panics

	func() {
		defer func() {
			if recover() == nil {
				t.Error("the join did not panic")
			}
		}()
		h.joinVoice(second, ch)
	}()
	first.shutdown() // nothing more is sent to it
	second.abandon()

	if media.Room(ch.ID) != nil {
		t.Fatal("a media peer stayed in the Talk after its connection was cleaned up")
	}
}

// A listener that panics on a leave (the audit log's Presence hook) does not
// keep the leave from the others: they would show the member in the Talk
// until they reload.
func TestPanickingPresenceListenerStillTellsTheOthersOfTheLeave(t *testing.T) {
	h := NewHub(nil, nil, nil, nil, openPerms())
	t.Cleanup(h.Close)
	alice := testClient(h, auth.User{ID: uuid.New(), Username: "alice"})
	bob := testClient(h, auth.User{ID: uuid.New(), Username: "bob"})
	ch := voiceCh()
	h.joinVoice(alice, ch)
	sentEvents(t, bob, events.VoiceStateUpdate)
	h.Presence = func(uuid.UUID, uuid.UUID, bool) { panic("presence hook bug") }

	func() {
		defer func() { _ = recover() }()
		h.removePresence(alice.User.ID, ch.ID)
	}()

	updates := sentEvents(t, bob, events.VoiceStateUpdate)
	if len(updates) != 1 || updates[0]["action"] != "leave" || updates[0]["user_id"] != alice.User.ID.String() {
		t.Fatalf("voice state updates after the leave = %+v", updates)
	}
}
