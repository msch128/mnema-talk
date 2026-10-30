package ws

import (
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
)

func twoSessions(t *testing.T, s bool) (*Hub, *Client, *Client) {
	t.Helper()
	var h *Hub
	if s {
		h = NewHub(nil, nil, newSeatTestSFU(t), nil)
	} else {
		h = NewHub(nil, nil, nil, nil)
	}
	user := auth.User{ID: uuid.New(), Username: "takeover"}
	a := testClient(h, user)
	b := testClient(h, user)
	t.Cleanup(func() { h.unregister(a); h.unregister(b) })
	return h, a, b
}

func seatedIn(c *Client, id uuid.UUID) bool {
	cur := c.currentVoice()
	return cur != nil && *cur == id
}

// A takeover in the Talk the other session sits in moves the voice: the old
// session is told why, keeps its connection, and the user never leaves the room.
func TestTakeoverInTheSameTalkMovesTheVoice(t *testing.T) {
	h, a, b := twoSessions(t, true)
	ch := voiceCh()
	h.joinVoice(a, ch)

	h.takeOverVoice(b, ch)

	if a.currentVoice() != nil || a.peer() != nil {
		t.Fatal("old session still holds voice")
	}
	kicked := sentEvents(t, a, "voice_kicked")
	if len(kicked) != 1 || kicked[0]["reason"] != "voice_taken_over" || kicked[0]["channel_id"] != ch.ID.String() {
		t.Fatalf("voice_kicked = %+v", kicked)
	}
	if !seatedIn(b, ch.ID) || b.peer() == nil || !inRoom(h, ch.ID, a.User.ID) {
		t.Fatal("new session is not seated")
	}
	if a.closed.Load() {
		t.Fatal("the old session's connection was closed")
	}
	a.SendEvent("message_create", map[string]any{"id": "m"})
	if got := sentEvents(t, a, "message_create"); len(got) != 1 {
		t.Fatalf("old session no longer receives text events: %+v", got)
	}
}

// From another Talk: the old Talk loses the user, announced like a leave.
func TestTakeoverFromAnotherTalkEndsTheOldSeat(t *testing.T) {
	h, a, b := twoSessions(t, true)
	old, next := voiceCh(), voiceCh()
	observer := testClient(h, auth.User{ID: uuid.New(), Username: "observer"})
	defer h.unregister(observer)
	h.joinVoice(a, old)
	sentEvents(t, observer, "voice_state_update")

	h.takeOverVoice(b, next)

	if a.currentVoice() != nil || a.peer() != nil || inRoom(h, old.ID, a.User.ID) {
		t.Fatal("old Talk still lists the user or the session")
	}
	if !seatedIn(b, next.ID) || !inRoom(h, next.ID, b.User.ID) {
		t.Fatal("new Talk does not hold the user")
	}
	kicked := sentEvents(t, a, "voice_kicked")
	if len(kicked) != 1 || kicked[0]["reason"] != "voice_taken_over" || kicked[0]["channel_id"] != old.ID.String() {
		t.Fatalf("voice_kicked = %+v", kicked)
	}
	left := false
	for _, ev := range sentEvents(t, observer, "voice_state_update") {
		left = left || (ev["action"] == "leave" && ev["channel_id"] == old.ID.String())
	}
	if !left {
		t.Fatal("others were not told the user left the old Talk")
	}
	if a.closed.Load() {
		t.Fatal("the old session's connection was closed")
	}
}

// A takeover whose media join fails ends nothing: the old session is the only
// one with a working call.
func TestFailedTakeoverLeavesTheOldSessionAlone(t *testing.T) {
	h, a, b := twoSessions(t, true)
	old, next := voiceCh(), voiceCh()
	h.joinVoice(a, old)
	peer := a.peer()
	if err := h.SFU.Close(); err != nil {
		t.Fatal(err)
	}

	h.takeOverVoice(b, next)

	if !seatedIn(a, old.ID) || a.peer() != peer || !inRoom(h, old.ID, a.User.ID) {
		t.Fatal("the failed takeover took the old session's call")
	}
	if kicked := sentEvents(t, a, "voice_kicked"); len(kicked) != 0 {
		t.Fatalf("old session was told: %+v", kicked)
	}
}

// Without a takeover a join in another Talk still leaves the other session in
// its own Talk; only the same Talk is handed over (as joined_elsewhere).
func TestPlainJoinKeepsTheOtherSessionInItsTalk(t *testing.T) {
	h, a, b := twoSessions(t, false)
	old, next := voiceCh(), voiceCh()
	h.joinVoice(a, old)
	h.joinVoice(b, next)
	if !seatedIn(a, old.ID) || !seatedIn(b, next.ID) {
		t.Fatal("a plain join ended the other session's voice")
	}
}

// A takeover with nothing to take (a reload's ghost, no other session) is an
// ordinary join.
func TestTakeoverWithoutAnOtherSessionIsAPlainJoin(t *testing.T) {
	h, a, b := twoSessions(t, true)
	ch := voiceCh()
	h.takeOverVoice(a, ch)
	if !seatedIn(a, ch.ID) || b.currentVoice() != nil {
		t.Fatal("unexpected seats")
	}
	if kicked := sentEvents(t, b, "voice_kicked"); len(kicked) != 0 {
		t.Fatalf("an idle session was told: %+v", kicked)
	}
}

// Two takeovers at the same moment leave exactly one session with voice, with
// the user still in exactly one Talk and no flapping between the two.
func TestSimultaneousTakeoversLeaveExactlyOneSession(t *testing.T) {
	for i := 0; i < 40; i++ {
		h, a, b := twoSessions(t, true)
		first, second := voiceCh(), voiceCh()
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); h.takeOverVoice(a, first) }()
		go func() { defer wg.Done(); h.takeOverVoice(b, second) }()
		wg.Wait()

		na, nb := a.currentVoice() != nil, b.currentVoice() != nil
		if na == nb {
			t.Fatalf("run %d: sessions with voice: a=%v b=%v", i, na, nb)
		}
		winner, room, lost := a, first, second
		if nb {
			winner, room, lost = b, second, first
		}
		if !seatedIn(winner, room.ID) || winner.peer() == nil || !inRoom(h, room.ID, winner.User.ID) {
			t.Fatalf("run %d: winner not fully seated", i)
		}
		if inRoom(h, lost.ID, winner.User.ID) {
			t.Fatalf("run %d: user still listed in the lost Talk", i)
		}
	}
}

// A client that reconnects after another device took over must not take the
// voice back by resuming; a resume the server still holds a stay for goes on.
func TestResumeAfterATakeoverDoesNotStealTheVoiceBack(t *testing.T) {
	h, a, b := twoSessions(t, true)
	a.setTab("tab-a")
	b.setTab("tab-b")
	ch := voiceCh()
	h.joinVoice(a, ch)
	h.takeOverVoice(b, ch)
	sentEvents(t, a, "voice_kicked")

	h.resumeVoice(a, ch)

	if !seatedIn(b, ch.ID) || a.currentVoice() != nil {
		t.Fatal("the resume took the voice back")
	}
	kicked := sentEvents(t, a, "voice_kicked")
	if len(kicked) != 1 || kicked[0]["reason"] != "voice_taken_over" {
		t.Fatalf("voice_kicked = %+v", kicked)
	}
}

func TestResumeWithAStayTheServerHeldIsAnOrdinaryRejoin(t *testing.T) {
	h, a, b := twoSessions(t, true)
	ch := voiceCh()
	h.joinVoice(a, ch)
	h.joinVoice(b, voiceCh())                // another session sits in a different Talk
	h.startGrace(voiceKey{a.User.ID, ch.ID}) // its socket dropped and the stay lingers
	a.setVoice(nil)

	h.resumeVoice(a, ch)

	if !seatedIn(a, ch.ID) {
		t.Fatal("a resume within the grace period was refused")
	}
}

// A network flap: the new socket resumes before the old one of the same user
// was unregistered. Nothing took the call over, so the resume succeeds.
func TestResumeBeforeTheStaleSocketGoesIsNotRefused(t *testing.T) {
	h, a, b := twoSessions(t, true)
	ch := voiceCh()
	h.joinVoice(a, ch)

	h.resumeVoice(b, ch)

	if !seatedIn(b, ch.ID) || b.peer() == nil {
		t.Fatal("the resume was refused because of the user's own stale socket")
	}
	if kicked := sentEvents(t, b, "voice_kicked"); len(kicked) != 0 {
		t.Fatalf("voice_kicked = %+v", kicked)
	}
}

// Joining the Talk again on purpose ends the mark of an earlier takeover.
func TestAJoinOnPurposeEndsTheTakeoverMark(t *testing.T) {
	h, a, b := twoSessions(t, true)
	a.setTab("tab-a")
	ch := voiceCh()
	h.joinVoice(a, ch)
	h.takeOverVoice(b, ch)
	if !h.taken.has(voiceKey{a.User.ID, ch.ID}, "tab-a") {
		t.Fatal("the takeover left no mark")
	}
	h.joinVoice(a, ch)
	if h.taken.has(voiceKey{a.User.ID, ch.ID}, "tab-a") {
		t.Fatal("a join on purpose left the mark")
	}
}

// The page that took the call over reconnects while its old socket is still
// seated: its resume must succeed, whatever the takeover marked.
func TestTheTakersReconnectIsNotRefused(t *testing.T) {
	h, a, b := twoSessions(t, true)
	a.setTab("tab-a")
	b.setTab("tab-b")
	taker2 := testClient(h, b.User)
	defer h.unregister(taker2)
	taker2.setTab("tab-b") // the same page, new socket
	ch := voiceCh()
	h.joinVoice(a, ch)
	h.takeOverVoice(b, ch)

	h.resumeVoice(taker2, ch)

	if !seatedIn(taker2, ch.ID) {
		t.Fatal("the taker's own reconnect was refused")
	}
	if kicked := sentEvents(t, taker2, "voice_kicked"); len(kicked) != 0 {
		t.Fatalf("voice_kicked = %+v", kicked)
	}
}
