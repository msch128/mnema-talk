package ws

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/sfu"
)

func newSeatTestSFU(t *testing.T) *sfu.SFU {
	t.Helper()
	s, err := sfu.NewSFU(0, 0, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// The SFU keeps one peer per user and room, so a second tab joining the room
// replaces the first tab's media: the first tab must be told, not left on a
// closed connection.
func TestSecondConnectionJoiningTheSameRoomEvictsTheFirstFromVoice(t *testing.T) {
	h := NewHub(nil, nil, newSeatTestSFU(t), nil)
	ch := voiceCh()
	user := auth.User{ID: uuid.New(), Username: "twotabs"}
	a := testClient(h, user)
	b := testClient(h, user)
	defer h.unregister(a)
	defer h.unregister(b)

	h.joinVoice(a, ch)
	if a.peer() == nil {
		t.Fatal("no peer for the first tab")
	}
	h.joinVoice(b, ch)

	if a.currentVoice() != nil || a.peer() != nil {
		t.Fatal("first tab still believes it owns the media")
	}
	kicked := sentEvents(t, a, "voice_kicked")
	if len(kicked) != 1 || kicked[0]["reason"] != "joined_elsewhere" || kicked[0]["channel_id"] != ch.ID.String() {
		t.Fatalf("voice_kicked to first tab = %+v", kicked)
	}
	if cur := b.currentVoice(); cur == nil || *cur != ch.ID || b.peer() == nil {
		t.Fatal("second tab did not get the seat")
	}
	if !inRoom(h, ch.ID, user.ID) {
		t.Fatal("the user left the room")
	}
}

// A second tab whose SFU join fails must not take the first tab's media away:
// the first tab keeps its seat and its peer, and presence stays.
func TestFailedJoinOfASecondTabLeavesTheFirstTabAlone(t *testing.T) {
	s := newSeatTestSFU(t)
	h := NewHub(nil, nil, s, nil)
	ch := voiceCh()
	user := auth.User{ID: uuid.New(), Username: "twotabsfail"}
	a := testClient(h, user)
	b := testClient(h, user)
	defer h.unregister(a)
	defer h.unregister(b)

	h.joinVoice(a, ch)
	peer := a.peer()
	if peer == nil {
		t.Fatal("no peer for the first tab")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	h.joinVoice(b, ch)

	if cur := a.currentVoice(); cur == nil || *cur != ch.ID || a.peer() != peer {
		t.Fatal("the failed join of the second tab took the seat or the peer from the first")
	}
	if kicked := sentEvents(t, a, "voice_kicked"); len(kicked) != 0 {
		t.Fatalf("first tab was kicked: %+v", kicked)
	}
	if kicked := sentEvents(t, b, "voice_kicked"); len(kicked) != 1 || kicked[0]["reason"] != "media_unavailable" {
		t.Fatalf("voice_kicked to second tab = %+v", kicked)
	}
	if !inRoom(h, ch.ID, user.ID) {
		t.Fatal("presence of the first tab was removed")
	}
}

// Without a media connection the member would be listed and counted as in the
// call while nobody can hear them: the seat is given up and the client told.
func TestFailedSFUJoinGivesUpTheSeatAndTellsTheClient(t *testing.T) {
	s := newSeatTestSFU(t)
	h := NewHub(nil, nil, s, nil)
	ch := voiceCh()
	c := testClient(h, auth.User{ID: uuid.New(), Username: "nomedia"})
	defer h.unregister(c)
	observer := testClient(h, auth.User{ID: uuid.New(), Username: "observer"})
	defer h.unregister(observer)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	h.joinVoice(c, ch)

	if inRoom(h, ch.ID, c.User.ID) || c.currentVoice() != nil {
		t.Fatal("member stayed seated without media")
	}
	kicked := sentEvents(t, c, "voice_kicked")
	if len(kicked) != 1 || kicked[0]["reason"] != "media_unavailable" {
		t.Fatalf("voice_kicked = %+v", kicked)
	}
	for _, ev := range sentEvents(t, observer, "voice_state_update") {
		if ev["action"] == "join" {
			t.Fatal("a join was announced for a member without media")
		}
	}
}

// A join that resumed a lingering presence and was then refused must leave the
// presence on a timer again, or nothing would ever remove it.
func TestRefusedResumeOfALingeringPresenceRestartsTheGrace(t *testing.T) {
	h := NewHub(nil, nil, nil, nil)
	h.VoiceGrace = time.Hour
	ch := voiceCh()
	uid := uuid.New()
	h.mu.Lock()
	h.initVoiceRoomLocked(ch.ID, time.Now())
	h.voice[ch.ID][uid] = auth.User{ID: uid}
	h.mu.Unlock()
	h.startGrace(voiceKey{uid, ch.ID})
	if len(h.takeUserGraces(uid)) != 1 {
		t.Fatal("no grace to take")
	}
	c := testClient(h, auth.User{ID: uid})
	defer h.unregister(c)

	h.restoreGraceIfAbandoned(c, ch.ID)

	h.mu.RLock()
	g := h.grace[voiceKey{uid, ch.ID}]
	h.mu.RUnlock()
	if g == nil {
		t.Fatal("presence has no grace timer")
	}
	g.timer.Stop()
}

// A mute written after the seat is gone (a kick between the read loop and the
// lock) must not survive to taint the next stay.
func TestMuteStateIsNotRecordedForAGoneSeat(t *testing.T) {
	h := NewHub(nil, nil, nil, nil)
	ch := voiceCh()
	c := testClient(h, auth.User{ID: uuid.New(), Username: "ghostmute"})
	defer h.unregister(c)
	h.joinVoice(c, ch)
	obs := testClient(h, auth.User{ID: uuid.New()})
	defer h.unregister(obs)
	// The seat vanishes while the client still holds its voice room.
	h.removePresence(c.User.ID, ch.ID)
	for len(obs.send) > 0 {
		<-obs.send
	}

	h.setMuteState(c, MuteState{Muted: true})

	if h.isMuted(c.User.ID, ch.ID) {
		t.Fatal("mute recorded for a seat that is gone")
	}
	if got := sentEvents(t, obs, "voice_mute_state"); len(got) != 0 {
		t.Fatalf("ghost mute event: %+v", got)
	}
}
