//go:build integration

package server

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/events"
)

// callFixture is a conversation between Alice and Bob in a fresh community (no
// Create-a-Talk lobby); each of them has a live socket that asked for
// conversations.
type callFixture struct {
	*dmFixture
	dm                 dmView
	alice, bob         *wsConn
	oldBob, strangerWS *wsConn
}

func newCallFixture(t *testing.T) *callFixture {
	t.Helper()
	f := &callFixture{dmFixture: newDMFixture(t, true)}
	f.dm = f.dmFixture.alice.mustDM(t, f.dmFixture.bob)
	wantStatus(t, "first words", f.dmFixture.alice.dmPost(f.dm.ID, "hello"), http.StatusCreated)
	f.alice = f.dmFixture.alice.dialLiveWSQuery(f.a.origin(), "?dm=1")
	f.bob = f.dmFixture.bob.dialLiveWSQuery(f.a.origin(), "?dm=1")
	f.oldBob = f.dmFixture.bob.dialLiveWS(f.a.origin())
	f.strangerWS = f.dmFixture.carol.dialLiveWSQuery(f.a.origin(), "?dm=1")
	return f
}

// start has Alice start a Talk in the conversation and returns its id.
func (f *callFixture) start(t *testing.T) uuid.UUID {
	t.Helper()
	res := f.dmFixture.alice.post("/api/dms/"+f.dm.ID.String()+"/talk", nil)
	wantStatus(t, "start a Talk", res, http.StatusCreated)
	var out chat.StartTalkResponse
	res.decode(t, &out)
	return out.ChannelID
}

func decodeEvent[T any](t *testing.T, raw json.RawMessage) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("event payload %s: %v", raw, err)
	}
	return out
}

func TestACallRingsTheConversationsMembersAndNobodyElse(t *testing.T) {
	f := newCallFixture(t)
	talk := f.start(t)

	raw, ok := f.bob.expect(events.DMCallRing, 3*time.Second)
	if !ok {
		t.Fatal("Bob's client is not told about the call")
	}
	ring := decodeEvent[chat.DMCallRing](t, raw)
	if ring.ConversationID != f.dm.ID || ring.TalkID != talk || ring.CallerID != f.dmFixture.alice.user.ID || ring.RingSeconds <= 0 {
		t.Fatalf("ring = %+v", ring)
	}
	if _, ok := f.alice.expect(events.DMCallRing, 3*time.Second); !ok {
		t.Error("the caller's own sessions hear it too, to show the calling state")
	}
	for name, ws := range map[string]*wsConn{"a client from before conversations": f.oldBob, "another member": f.strangerWS} {
		if raw, got := ws.expect(events.DMCallRing, 700*time.Millisecond); got {
			t.Errorf("%s heard the call: %s", name, raw)
		}
	}
	// The Talk of the conversation is for its two members: nobody else sees it, the Owner included.
	for name, c := range map[string]*client{"alice": f.dmFixture.alice, "bob": f.dmFixture.bob, "the Owner": f.admin, "carol": f.dmFixture.carol} {
		var hierarchy struct {
			Uncategorized []struct {
				ID uuid.UUID `json:"id"`
			} `json:"uncategorized"`
		}
		c.get("/api/channels").decode(t, &hierarchy)
		seen := false
		for _, ch := range hierarchy.Uncategorized {
			seen = seen || ch.ID == talk
		}
		if want := name == "alice" || name == "bob"; seen != want {
			t.Errorf("%s sees the Talk of the conversation: %v, want %v", name, seen, want)
		}
	}
	// A client that connects while it rings finds the call in the list.
	var listed []struct {
		ID   uuid.UUID    `json:"id"`
		Call *chat.DMCall `json:"call"`
	}
	f.dmFixture.bob.get("/api/dms").decode(t, &listed)
	if len(listed) != 1 || listed[0].Call == nil || !listed[0].Call.Ringing || listed[0].Call.TalkID != talk {
		t.Fatalf("list = %+v", listed)
	}
}

func TestJoiningStopsTheRingEverywhereAndHangingUpFirstLeavesAMissedCall(t *testing.T) {
	f := newCallFixture(t)
	talk := f.start(t)
	aliceConn, _, err := f.dmFixture.alice.dialWS(f.a.origin())
	if err != nil {
		t.Fatal(err)
	}
	aliceConn.send(events.ClientVoiceJoin, map[string]any{"channel_id": talk})
	bobTab2 := f.dmFixture.bob.dialLiveWSQuery(f.a.origin(), "?dm=1")
	bobConn := f.bob
	bobConn.send(events.ClientVoiceJoin, map[string]any{"channel_id": talk})

	for name, ws := range map[string]*wsConn{"Bob's other tab": bobTab2, "Bob's tab": f.bob, "Alice": f.alice} {
		raw, ok := ws.expect(events.DMCallEnd, 3*time.Second)
		if !ok {
			t.Fatalf("%s is not told the call was answered", name)
		}
		end := decodeEvent[chat.DMCallEnd](t, raw)
		if end.Reason != chat.CallAnswered || !end.Active || end.UserID == nil || *end.UserID != f.dmFixture.bob.user.ID {
			t.Fatalf("%s: end = %+v", name, end)
		}
	}
}

func TestACallTheCallerCancelsLeavesAMissedCallInTheConversation(t *testing.T) {
	f := newCallFixture(t)
	talk := f.start(t)
	aliceConn, _, err := f.dmFixture.alice.dialWS(f.a.origin())
	if err != nil {
		t.Fatal(err)
	}
	aliceConn.send(events.ClientVoiceJoin, map[string]any{"channel_id": talk})
	if _, ok := f.bob.expect(events.DMCallRing, 3*time.Second); !ok {
		t.Fatal("no ring")
	}
	aliceConn.send(events.ClientVoiceLeave, nil)

	got := f.bob.collect(5*time.Second, events.DMCallEnd)
	raw, ok := got[events.DMCallEnd]
	if !ok {
		t.Fatal("Bob is not told the caller hung up")
	}
	end := decodeEvent[chat.DMCallEnd](t, raw)
	if end.Reason != chat.CallCancelled || end.Active || end.MessageID == nil {
		t.Fatalf("end = %+v", end)
	}
	var page []struct {
		ID     uuid.UUID `json:"id"`
		UserID uuid.UUID `json:"user_id"`
		Embeds []struct {
			Call string `json:"call"`
		} `json:"embeds"`
	}
	f.dmFixture.bob.get("/api/channels/"+f.dm.ID.String()+"/messages").decode(t, &page)
	found := false
	for _, m := range page {
		if m.ID == *end.MessageID && m.UserID == f.dmFixture.alice.user.ID && len(m.Embeds) == 1 && m.Embeds[0].Call == "missed" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the missed call is not in the conversation: %+v", page)
	}
	var listed []struct {
		UnreadCount int `json:"unread_count"`
		LastMessage struct {
			Call string `json:"call"`
		} `json:"last_message"`
	}
	f.dmFixture.bob.get("/api/dms").decode(t, &listed)
	if len(listed) != 1 || listed[0].UnreadCount < 2 || listed[0].LastMessage.Call != "missed" {
		t.Fatalf("the invitation and the missed call are unread, and the list previews the call: %+v", listed)
	}
}

func TestWhatIsWrittenInAConversationsTalkIsForItsMembersOnly(t *testing.T) {
	f := newCallFixture(t)
	talk := f.start(t)
	res := f.dmFixture.alice.post("/api/channels/"+talk.String()+"/messages", map[string]any{"content": "zebracrossing plans"})
	wantStatus(t, "alice writes next to the Talk", res, http.StatusCreated)
	found := func(c *client) int {
		out, status := c.search(t, map[string]string{"q": "zebracrossing"})
		if status != http.StatusOK {
			t.Fatalf("search: %d", status)
		}
		return len(out.Messages)
	}
	if found(f.dmFixture.alice) != 1 || found(f.dmFixture.bob) != 1 {
		t.Error("the two members find it")
	}
	if found(f.admin) != 0 || found(f.dmFixture.carol) != 0 {
		t.Error("the Owner or a stranger found a message of a conversation's Talk")
	}
	wantStatus(t, "the Owner reads it", f.admin.get("/api/channels/"+talk.String()+"/messages"), http.StatusNotFound)
}

func TestWhoCalledWhomIsNotInTheAuditLog(t *testing.T) {
	f := newCallFixture(t)
	talk := f.start(t)
	conn, _, err := f.dmFixture.alice.dialWS(f.a.origin())
	if err != nil {
		t.Fatal(err)
	}
	conn.send(events.ClientVoiceJoin, map[string]any{"channel_id": talk})
	if _, ok := f.bob.expect(events.DMCallRing, 3*time.Second); !ok {
		t.Fatal("no ring")
	}
	conn.send(events.ClientVoiceLeave, nil)
	if _, ok := f.bob.expect(events.DMCallEnd, 5*time.Second); !ok {
		t.Fatal("the call did not end")
	}
	// A control: an ordinary Talk is audited, and its entries come after the call's.
	open := f.a.createChannel(f.admin, "Lounge", "voice")
	other, _, err := f.dmFixture.bob.dialWS(f.a.origin())
	if err != nil {
		t.Fatal(err)
	}
	other.send(events.ClientVoiceJoin, map[string]any{"channel_id": open})
	waitForAudit(t, f.admin, "action=voice.join", 1)
	for _, e := range auditEntries(t, f.admin, "limit=200").Entries {
		if e.TargetID == talk.String() {
			t.Errorf("the Owner reads %s of a conversation's Talk", e.Action)
		}
	}
}
