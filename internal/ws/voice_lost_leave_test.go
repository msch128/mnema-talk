package ws

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mnema-xyz/talk/internal/auth"
	"github.com/mnema-xyz/talk/internal/events"
)

// A voice_leave over a new connection ending a lost connection's lingering seat.
func TestVoiceLeaveEndsLostSeatMatchingTab(t *testing.T) {
	h := NewHub(nil, nil, nil, nil, openPerms())
	h.VoiceGrace = 5 * time.Second
	t.Cleanup(h.Close)
	ch := voiceCh()
	user := auth.User{ID: uuid.New(), Username: "alice"}
	conn1 := testClient(h, user)
	conn1.setTab("tab-T")
	h.joinVoice(conn1, ch)

	observer := testClient(h, auth.User{ID: uuid.New(), Username: "observer"})
	h.joinVoice(observer, ch)
	sentEvents(t, observer, "")

	h.unregister(conn1)
	if graceCount(h) != 1 {
		t.Fatal("grace did not start")
	}
	if !inRoom(h, ch.ID, user.ID) {
		t.Fatal("user left room immediately on drop")
	}

	conn2 := testClient(h, user)
	payload := []byte(fmt.Sprintf(`{"channel_id":"%s","tab":"tab-T"}`, ch.ID))
	conn2.handle(events.ClientVoiceLeave, payload)

	if inRoom(h, ch.ID, user.ID) {
		t.Fatal("presence not gone at once after lost leave")
	}
	if graceCount(h) != 0 {
		t.Fatal("grace timer was not cancelled")
	}
	leaves := sentEvents(t, observer, "voice_state_update")
	if len(leaves) != 1 || leaves[0]["action"] != "leave" || leaves[0]["user_id"] != user.ID.String() {
		t.Fatalf("want 1 leave announcement, got %+v", leaves)
	}
}

func TestVoiceLeaveTabMismatchKeepsGrace(t *testing.T) {
	h := NewHub(nil, nil, nil, nil, openPerms())
	h.VoiceGrace = 50 * time.Millisecond
	t.Cleanup(h.Close)
	ch := voiceCh()
	user := auth.User{ID: uuid.New(), Username: "alice"}
	conn1 := testClient(h, user)
	conn1.setTab("tab-T")
	h.joinVoice(conn1, ch)

	observer := testClient(h, auth.User{ID: uuid.New(), Username: "observer"})
	h.joinVoice(observer, ch)
	sentEvents(t, observer, "")

	h.unregister(conn1)
	if graceCount(h) != 1 {
		t.Fatal("grace did not start")
	}

	conn2 := testClient(h, user)
	payload := []byte(fmt.Sprintf(`{"channel_id":"%s","tab":"other-tab"}`, ch.ID))
	conn2.handle(events.ClientVoiceLeave, payload)

	if graceCount(h) != 1 {
		t.Fatal("grace cancelled on tab mismatch")
	}
	if !inRoom(h, ch.ID, user.ID) {
		t.Fatal("seat removed on tab mismatch")
	}
	if len(sentEvents(t, observer, "voice_state_update")) != 0 {
		t.Fatal("leave announced on tab mismatch")
	}

	waitSeat(t, h, ch.ID, user.ID, false)
	if graceCount(h) != 0 {
		t.Fatal("grace not cleaned up after expiry")
	}
}

func TestVoiceLeaveWhenAnotherConnectionSeatedKeepsSeat(t *testing.T) {
	h := NewHub(nil, nil, nil, nil, openPerms())
	h.VoiceGrace = 5 * time.Second
	t.Cleanup(h.Close)
	ch := voiceCh()
	user := auth.User{ID: uuid.New(), Username: "alice"}
	conn1 := testClient(h, user)
	conn1.setTab("tab-T")
	h.joinVoice(conn1, ch)

	observer := testClient(h, auth.User{ID: uuid.New(), Username: "observer"})
	h.joinVoice(observer, ch)
	sentEvents(t, observer, "")

	h.unregister(conn1)
	if graceCount(h) != 1 {
		t.Fatal("grace did not start")
	}

	connSibling := testClient(h, user)
	connSibling.setVoice(&ch.ID)

	conn2 := testClient(h, user)
	payload := []byte(fmt.Sprintf(`{"channel_id":"%s","tab":"tab-T"}`, ch.ID))
	conn2.handle(events.ClientVoiceLeave, payload)

	if !inRoom(h, ch.ID, user.ID) {
		t.Fatal("seat was removed while sibling connection seated")
	}
	if graceCount(h) != 1 {
		t.Fatal("grace was cancelled while sibling connection seated")
	}
	if len(sentEvents(t, observer, "voice_state_update")) != 0 {
		t.Fatal("leave was announced while sibling connection seated")
	}
}

func TestVoiceLeaveFromDifferentUserDoesNothing(t *testing.T) {
	h := NewHub(nil, nil, nil, nil, openPerms())
	h.VoiceGrace = 5 * time.Second
	t.Cleanup(h.Close)
	ch := voiceCh()
	userA := auth.User{ID: uuid.New(), Username: "alice"}
	connA := testClient(h, userA)
	connA.setTab("tab-T")
	h.joinVoice(connA, ch)

	observer := testClient(h, auth.User{ID: uuid.New(), Username: "observer"})
	h.joinVoice(observer, ch)
	sentEvents(t, observer, "")

	h.unregister(connA)
	if graceCount(h) != 1 {
		t.Fatal("grace did not start for user A")
	}

	userB := auth.User{ID: uuid.New(), Username: "bob"}
	connB := testClient(h, userB)
	payload := []byte(fmt.Sprintf(`{"channel_id":"%s","tab":"tab-T"}`, ch.ID))
	connB.handle(events.ClientVoiceLeave, payload)

	if !inRoom(h, ch.ID, userA.ID) {
		t.Fatal("user A was removed by user B's leave")
	}
	if graceCount(h) != 1 {
		t.Fatal("grace was touched by user B's leave")
	}
	if len(sentEvents(t, observer, "voice_state_update")) != 0 {
		t.Fatal("leave announced for user A on user B's leave")
	}
}

func TestVoiceLeaveWithoutPendingGraceIsNoop(t *testing.T) {
	h := NewHub(nil, nil, nil, nil, openPerms())
	t.Cleanup(h.Close)
	ch := voiceCh()
	user := auth.User{ID: uuid.New(), Username: "alice"}

	observer := testClient(h, auth.User{ID: uuid.New(), Username: "observer"})
	h.joinVoice(observer, ch)
	sentEvents(t, observer, "")

	conn := testClient(h, user)
	payload := []byte(fmt.Sprintf(`{"channel_id":"%s","tab":"tab-T"}`, ch.ID))
	conn.handle(events.ClientVoiceLeave, payload)

	if inRoom(h, ch.ID, user.ID) {
		t.Fatal("user unexpectedly in room")
	}
	if len(sentEvents(t, observer, "voice_state_update")) != 0 {
		t.Fatal("unexpected leave announcement without pending grace")
	}
}

func TestVoiceLeaveWithEmptyTabOrZeroChannelIsNoop(t *testing.T) {
	h := NewHub(nil, nil, nil, nil, openPerms())
	h.VoiceGrace = 5 * time.Second
	t.Cleanup(h.Close)
	ch := voiceCh()
	user := auth.User{ID: uuid.New(), Username: "alice"}
	conn1 := testClient(h, user)
	conn1.setTab("tab-T")
	h.joinVoice(conn1, ch)

	observer := testClient(h, auth.User{ID: uuid.New(), Username: "observer"})
	h.joinVoice(observer, ch)
	sentEvents(t, observer, "")

	h.unregister(conn1)
	if graceCount(h) != 1 {
		t.Fatal("grace did not start")
	}

	conn2 := testClient(h, user)

	conn2.handle(events.ClientVoiceLeave, []byte(`{"channel_id":"00000000-0000-0000-0000-000000000000","tab":"tab-T"}`))
	if graceCount(h) != 1 || !inRoom(h, ch.ID, user.ID) {
		t.Fatal("grace or seat affected by zero channel id")
	}

	conn2.handle(events.ClientVoiceLeave, []byte(fmt.Sprintf(`{"channel_id":"%s","tab":""}`, ch.ID)))
	if graceCount(h) != 1 || !inRoom(h, ch.ID, user.ID) {
		t.Fatal("grace or seat affected by empty tab")
	}

	longTab := strings.Repeat("x", 65)
	conn2.handle(events.ClientVoiceLeave, []byte(fmt.Sprintf(`{"channel_id":"%s","tab":"%s"}`, ch.ID, longTab)))
	if graceCount(h) != 1 || !inRoom(h, ch.ID, user.ID) {
		t.Fatal("grace or seat affected by oversized tab")
	}

	// A seat left with oversized tab cannot be ended by oversized tab.
	take := h.takeGrace(voiceKey{user.ID, ch.ID})
	if take != nil {
		take.timer.Stop()
	}
	connLong := testClient(h, user)
	connLong.setTab(longTab)
	h.joinVoice(connLong, ch)
	h.unregister(connLong)
	if graceCount(h) != 1 {
		t.Fatal("grace did not start for long tab")
	}
	conn2.handle(events.ClientVoiceLeave, []byte(fmt.Sprintf(`{"channel_id":"%s","tab":"%s"}`, ch.ID, longTab)))
	if graceCount(h) != 1 || !inRoom(h, ch.ID, user.ID) {
		t.Fatal("oversized tab was accepted")
	}

	// When connection joined without a tab (grace tab is empty), leave with empty tab must not end it.
	take = h.takeGrace(voiceKey{user.ID, ch.ID})
	if take != nil {
		take.timer.Stop()
	}
	connEmpty := testClient(h, user)
	h.joinVoice(connEmpty, ch)
	h.unregister(connEmpty)
	if graceCount(h) != 1 {
		t.Fatal("grace did not start for connection without tab")
	}
	conn2.handle(events.ClientVoiceLeave, []byte(fmt.Sprintf(`{"channel_id":"%s","tab":""}`, ch.ID)))
	if graceCount(h) != 1 || !inRoom(h, ch.ID, user.ID) {
		t.Fatal("grace ended for empty tab on grace without tab")
	}

	if len(sentEvents(t, observer, "voice_state_update")) != 0 {
		t.Fatal("unexpected leave announcement on invalid payload")
	}
}

func TestVoiceLeaveEndsStillSeatedLostConnection(t *testing.T) {
	h := NewHub(nil, nil, nil, nil, openPerms())
	h.VoiceGrace = 5 * time.Second
	t.Cleanup(h.Close)
	ch := voiceCh()
	user := auth.User{ID: uuid.New(), Username: "alice"}
	conn1 := testClient(h, user)
	conn1.setTab("tab-T")
	h.joinVoice(conn1, ch)

	observer := testClient(h, auth.User{ID: uuid.New(), Username: "observer"})
	h.joinVoice(observer, ch)
	sentEvents(t, observer, "")

	conn2 := testClient(h, user)
	payload := []byte(fmt.Sprintf(`{"channel_id":"%s","tab":"tab-T"}`, ch.ID))
	conn2.handle(events.ClientVoiceLeave, payload)

	if inRoom(h, ch.ID, user.ID) {
		t.Fatal("user still in room after leave")
	}
	if conn1.currentVoice() != nil {
		t.Fatal("conn1 still seated")
	}
	if graceCount(h) != 0 {
		t.Fatal("unexpected grace left")
	}
	leaves := sentEvents(t, observer, "voice_state_update")
	if len(leaves) != 1 || leaves[0]["action"] != "leave" || leaves[0]["user_id"] != user.ID.String() {
		t.Fatalf("want exactly 1 leave announcement, got %+v", leaves)
	}
}

func TestVoiceLeaveDifferentTabOnStillSeatedConnectionUntouched(t *testing.T) {
	h := NewHub(nil, nil, nil, nil, openPerms())
	h.VoiceGrace = 5 * time.Second
	t.Cleanup(h.Close)
	ch := voiceCh()
	user := auth.User{ID: uuid.New(), Username: "alice"}
	conn1 := testClient(h, user)
	conn1.setTab("other-tab")
	h.joinVoice(conn1, ch)

	observer := testClient(h, auth.User{ID: uuid.New(), Username: "observer"})
	h.joinVoice(observer, ch)
	sentEvents(t, observer, "")

	conn2 := testClient(h, user)
	payload := []byte(fmt.Sprintf(`{"channel_id":"%s","tab":"tab-T"}`, ch.ID))
	conn2.handle(events.ClientVoiceLeave, payload)

	if !inRoom(h, ch.ID, user.ID) || conn1.currentVoice() == nil {
		t.Fatal("conn1 seat removed despite tab mismatch")
	}
	if len(sentEvents(t, observer, "voice_state_update")) != 0 {
		t.Fatal("unexpected leave announcement on tab mismatch")
	}
}

func TestVoiceLeaveDifferentTalkOnStillSeatedConnectionUntouched(t *testing.T) {
	h := NewHub(nil, nil, nil, nil, openPerms())
	h.VoiceGrace = 5 * time.Second
	t.Cleanup(h.Close)
	chA := voiceCh()
	chB := voiceCh()
	user := auth.User{ID: uuid.New(), Username: "alice"}
	conn1 := testClient(h, user)
	conn1.setTab("tab-T")
	h.joinVoice(conn1, chB)

	observer := testClient(h, auth.User{ID: uuid.New(), Username: "observer"})
	h.joinVoice(observer, chB)
	sentEvents(t, observer, "")

	conn2 := testClient(h, user)
	payload := []byte(fmt.Sprintf(`{"channel_id":"%s","tab":"tab-T"}`, chA.ID))
	conn2.handle(events.ClientVoiceLeave, payload)

	cur := conn1.currentVoice()
	if !inRoom(h, chB.ID, user.ID) || cur == nil || *cur != chB.ID {
		t.Fatal("conn1 seat in Talk B removed on leave for Talk A")
	}
	if len(sentEvents(t, observer, "voice_state_update")) != 0 {
		t.Fatal("unexpected leave announcement for Talk B")
	}
}

func TestVoiceLeaveAnotherUserWithSameTabUntouched(t *testing.T) {
	h := NewHub(nil, nil, nil, nil, openPerms())
	h.VoiceGrace = 5 * time.Second
	t.Cleanup(h.Close)
	ch := voiceCh()
	userA := auth.User{ID: uuid.New(), Username: "alice"}
	connA := testClient(h, userA)
	connA.setTab("tab-T")
	h.joinVoice(connA, ch)

	observer := testClient(h, auth.User{ID: uuid.New(), Username: "observer"})
	h.joinVoice(observer, ch)
	sentEvents(t, observer, "")

	userB := auth.User{ID: uuid.New(), Username: "bob"}
	connB := testClient(h, userB)
	payload := []byte(fmt.Sprintf(`{"channel_id":"%s","tab":"tab-T"}`, ch.ID))
	connB.handle(events.ClientVoiceLeave, payload)

	if !inRoom(h, ch.ID, userA.ID) || connA.currentVoice() == nil {
		t.Fatal("user A seat removed by user B leave")
	}
	if len(sentEvents(t, observer, "voice_state_update")) != 0 {
		t.Fatal("unexpected leave announcement on different user leave")
	}
}

func TestRestoredGraceCarriesTab(t *testing.T) {
	h := NewHub(nil, nil, nil, nil, openPerms())
	h.VoiceGrace = 5 * time.Second
	t.Cleanup(h.Close)
	ch := voiceCh()
	user := auth.User{ID: uuid.New(), Username: "alice"}
	conn1 := testClient(h, user)
	conn1.setTab("tab-T")
	h.joinVoice(conn1, ch)

	observer := testClient(h, auth.User{ID: uuid.New(), Username: "observer"})
	h.joinVoice(observer, ch)
	sentEvents(t, observer, "")

	h.unregister(conn1)
	if graceCount(h) != 1 {
		t.Fatal("grace did not start")
	}

	conn2 := testClient(h, user)
	rejoin, tab, _ := h.leaveOtherRooms(conn2, ch.ID)
	if !rejoin || tab != "tab-T" {
		t.Fatalf("want rejoin=true and tab-T, got rejoin=%t tab=%q", rejoin, tab)
	}
	if graceCount(h) != 0 {
		t.Fatal("grace not taken by leaveOtherRooms")
	}

	h.restoreGraceIfAbandoned(conn2, ch.ID, tab)
	if graceCount(h) != 1 {
		t.Fatal("grace was not restored")
	}

	conn3 := testClient(h, user)
	payload := []byte(fmt.Sprintf(`{"channel_id":"%s","tab":"tab-T"}`, ch.ID))
	conn3.handle(events.ClientVoiceLeave, payload)

	if inRoom(h, ch.ID, user.ID) {
		t.Fatal("user still in room after leave matching restored grace")
	}
	if graceCount(h) != 0 {
		t.Fatal("restored grace was not cancelled")
	}
	leaves := sentEvents(t, observer, "voice_state_update")
	if len(leaves) != 1 || leaves[0]["action"] != "leave" || leaves[0]["user_id"] != user.ID.String() {
		t.Fatalf("want 1 leave announcement, got %+v", leaves)
	}
}

func TestGraceWithEmptyTabNeverMatches(t *testing.T) {
	h := NewHub(nil, nil, nil, nil, openPerms())
	h.VoiceGrace = 5 * time.Second
	t.Cleanup(h.Close)
	ch := voiceCh()
	user := auth.User{ID: uuid.New(), Username: "alice"}

	h.mu.Lock()
	h.initVoiceRoomLocked(ch.ID, time.Now())
	h.voice[ch.ID][user.ID] = user
	h.mu.Unlock()
	h.startGrace(voiceKey{user.ID, ch.ID}, nil)
	if graceCount(h) != 1 {
		t.Fatal("grace did not start")
	}

	conn := testClient(h, user)
	payload := []byte(fmt.Sprintf(`{"channel_id":"%s","tab":"given-tab"}`, ch.ID))
	conn.handle(events.ClientVoiceLeave, payload)

	if graceCount(h) != 1 || !inRoom(h, ch.ID, user.ID) {
		t.Fatal("grace with empty tab ended by given tab")
	}

	h.leaveLostVoice(user.ID, ch.ID, "", conn.logger())
	if graceCount(h) != 1 || !inRoom(h, ch.ID, user.ID) {
		t.Fatal("grace with empty tab ended by empty tab in leaveLostVoice")
	}
}

func TestVoiceLeaveWithoutTabDoesNotEndSeatWithoutTab(t *testing.T) {
	h := NewHub(nil, nil, nil, nil, openPerms())
	t.Cleanup(h.Close)
	ch := voiceCh()
	user := auth.User{ID: uuid.New(), Username: "alice"}
	conn1 := testClient(h, user)
	h.joinVoice(conn1, ch)

	conn2 := testClient(h, user)
	payload := []byte(fmt.Sprintf(`{"channel_id":"%s"}`, ch.ID))
	conn2.handle(events.ClientVoiceLeave, payload)

	if !inRoom(h, ch.ID, user.ID) || conn1.currentVoice() == nil {
		t.Fatal("seat without tab was ended by leave without tab")
	}
}

func TestVoiceLeaveFromSeatedConnectionLeavesOnlyOwnTalkAndPreservesGrace(t *testing.T) {
	h := NewHub(nil, nil, nil, nil, openPerms())
	h.VoiceGrace = 5 * time.Second
	t.Cleanup(h.Close)
	chA := voiceCh()
	chB := voiceCh()
	user := auth.User{ID: uuid.New(), Username: "alice"}

	// connB is seated in Talk B
	connB := testClient(h, user)
	h.joinVoice(connB, chB)

	// Talk A has a grace for alice
	h.mu.Lock()
	h.initVoiceRoomLocked(chA.ID, time.Now())
	h.voice[chA.ID][user.ID] = user
	h.mu.Unlock()
	h.startGrace(voiceKey{user.ID, chA.ID}, nil, "tab-A")

	observerA := testClient(h, auth.User{ID: uuid.New(), Username: "obsA"})
	h.joinVoice(observerA, chA)
	sentEvents(t, observerA, "")

	if graceCount(h) != 1 {
		t.Fatal("grace in Talk A did not start")
	}

	// connB sends voice_leave targeting Talk A and tab-A
	payload := []byte(fmt.Sprintf(`{"channel_id":"%s","tab":"tab-A"}`, chA.ID))
	connB.handle(events.ClientVoiceLeave, payload)

	// connB left Talk B
	if connB.currentVoice() != nil || inRoom(h, chB.ID, user.ID) {
		t.Fatal("connB did not leave Talk B")
	}
	// Talk A's grace is preserved
	if graceCount(h) != 1 || !inRoom(h, chA.ID, user.ID) {
		t.Fatal("Talk A grace was affected by leave from seated connection in Talk B")
	}
	for _, l := range sentEvents(t, observerA, "voice_state_update") {
		if l["channel_id"] == chA.ID.String() {
			t.Fatal("unexpected leave announcement in Talk A")
		}
	}
}

func TestVoiceLeaveJoinRacingLeaveKeepsSeat(t *testing.T) {
	h := NewHub(nil, nil, nil, nil, openPerms())
	h.VoiceGrace = 5 * time.Second
	t.Cleanup(h.Close)
	ch := voiceCh()
	user := auth.User{ID: uuid.New(), Username: "alice"}
	conn1 := testClient(h, user)
	conn1.setTab("tab-T")
	h.joinVoice(conn1, ch)

	observer := testClient(h, auth.User{ID: uuid.New(), Username: "observer"})
	h.joinVoice(observer, ch)
	sentEvents(t, observer, "")

	h.unregister(conn1)
	if graceCount(h) != 1 {
		t.Fatal("grace did not start")
	}

	h.graceKeep = func(key voiceKey) bool {
		return key.user == user.ID && key.ch == ch.ID
	}

	conn2 := testClient(h, user)
	payload := []byte(fmt.Sprintf(`{"channel_id":"%s","tab":"tab-T"}`, ch.ID))
	conn2.handle(events.ClientVoiceLeave, payload)

	if !inRoom(h, ch.ID, user.ID) {
		t.Fatal("seat was removed despite racing join keeping it")
	}
	if len(sentEvents(t, observer, "voice_state_update")) != 0 {
		t.Fatal("unexpected leave announced when racing join keeps seat")
	}
}
