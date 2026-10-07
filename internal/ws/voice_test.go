package ws

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/sfu"
)

func testClient(h *Hub, u auth.User) *Client {
	c := newClient(h, nil, u, 0)
	c.send = make(chan []byte, 1024)
	h.register(c)
	return c
}

func voiceCh() *chat.ChannelInfo {
	return &chat.ChannelInfo{ID: uuid.New(), Type: chat.ChannelTypeVoice}
}

func inRoom(h *Hub, chID, userID uuid.UUID) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	_, ok := h.voice[chID][userID]
	return ok
}

// drainTypes empties c's queue and returns the event types in order.
func drainTypes(t *testing.T, c *Client) []string {
	t.Helper()
	var out []string
	for len(c.send) > 0 {
		var ev Event
		if err := json.Unmarshal(<-c.send, &ev); err != nil {
			t.Fatal(err)
		}
		out = append(out, ev.Type)
	}
	return out
}

func count(types []string, want string) int {
	n := 0
	for _, t := range types {
		if t == want {
			n++
		}
	}
	return n
}

// An admin kick racing a join must never leave a live SFU peer behind that
// nobody sees in the room (a silent eavesdropper).
func TestKickRacingJoinLeavesNoSFUPeer(t *testing.T) {
	voiceSFU, err := sfu.NewSFU(0, 0, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	h := NewHub(nil, nil, voiceSFU, nil)
	ch := voiceCh()
	user := auth.User{ID: uuid.New(), Username: "eve"}
	c := testClient(h, user)
	defer h.unregister(c)

	for i := 0; i < 20; i++ {
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); h.joinVoice(c, ch) }()
		go func() { defer wg.Done(); h.KickFromVoice(user.ID) }()
		wg.Wait()
		h.KickFromVoice(user.ID)
		if room := voiceSFU.Room(ch.ID); room != nil {
			t.Fatalf("iteration %d: SFU room still has a peer after the kick", i)
		}
		if inRoom(h, ch.ID, user.ID) {
			t.Fatalf("iteration %d: still listed in the room after the kick", i)
		}
		drainTypes(t, c)
	}
}

// Two tabs of one user in the same room: one leaving must keep the user
// listed while the other still listens.
func TestLeaveKeepsPresenceWhileAnotherTabListens(t *testing.T) {
	h := NewHub(nil, nil, nil, nil)
	ch := voiceCh()
	user := auth.User{ID: uuid.New(), Username: "two-tabs"}
	a, b := testClient(h, user), testClient(h, user)
	h.joinVoice(a, ch)
	h.joinVoice(b, ch)

	h.leaveCurrentVoice(a)
	if !inRoom(h, ch.ID, user.ID) {
		t.Fatal("user dropped from the room while the second tab is still in it")
	}
	h.leaveCurrentVoice(b)
	if inRoom(h, ch.ID, user.ID) {
		t.Fatal("user still listed after the last tab left")
	}
}

// Join time and mute state belong to one room: two tabs in two rooms do
// not share them.
func TestVoiceTimeAndMuteArePerRoom(t *testing.T) {
	h := NewHub(nil, nil, nil, nil)
	ch1, ch2 := voiceCh(), voiceCh()
	user := auth.User{ID: uuid.New(), Username: "split"}
	a, b := testClient(h, user), testClient(h, user)

	h.joinVoice(a, ch1)
	first := h.voiceSnapshot()[ch1.ID][user.ID].JoinedAt
	time.Sleep(5 * time.Millisecond)
	h.joinVoice(b, ch2)
	a.handle("voice_mute_state", []byte(`{"muted":true}`))

	snap := h.voiceSnapshot()
	if !snap[ch1.ID][user.ID].JoinedAt.Equal(first) {
		t.Fatal("joining a second room changed the first room's join time")
	}
	if !snap[ch2.ID][user.ID].JoinedAt.After(first) {
		t.Fatal("second room did not get its own join time")
	}
	if !snap[ch1.ID][user.ID].Muted || snap[ch2.ID][user.ID].Muted {
		t.Fatalf("mute state leaked across rooms: %+v / %+v", snap[ch1.ID][user.ID], snap[ch2.ID][user.ID])
	}

	h.leaveCurrentVoice(b)
	if _, ok := h.voiceSnapshot()[ch1.ID][user.ID]; !ok {
		t.Fatal("leaving one room ended the stay in the other")
	}
	h.mu.RLock()
	_, since := h.voiceSince[voiceKey{user.ID, ch1.ID}]
	h.mu.RUnlock()
	if !since {
		t.Fatal("leaving one room dropped the other room's join time")
	}
}

// Voice payloads follow profile changes made after the socket connected.
func TestVoicePayloadsFollowUserUpdates(t *testing.T) {
	h := NewHub(nil, nil, nil, nil)
	ch := voiceCh()
	user := auth.User{ID: uuid.New(), Username: "anna", DisplayName: "Anna"}
	c := testClient(h, user)
	watcher := testClient(h, auth.User{ID: uuid.New(), Username: "w"})

	renamed := user
	renamed.DisplayName = "Anna Neu"
	h.Broadcast("user_update", renamed.Public())
	drainTypes(t, watcher)

	h.joinVoice(c, ch)
	var join struct {
		Payload struct {
			User VoiceUser `json:"user"`
		} `json:"payload"`
	}
	found := false
	for len(watcher.send) > 0 {
		raw := <-watcher.send
		var ev Event
		_ = json.Unmarshal(raw, &ev)
		if ev.Type == "voice_state_update" {
			if err := json.Unmarshal(raw, &join); err != nil {
				t.Fatal(err)
			}
			found = true
		}
	}
	if !found || join.Payload.User.DisplayName != "Anna Neu" {
		t.Fatalf("join announced %q, want the updated name", join.Payload.User.DisplayName)
	}

	renamed.DisplayName = "Anna Neuer"
	h.Broadcast("user_update", &renamed)
	if got := h.voiceSnapshot()[ch.ID][user.ID].DisplayName; got != "Anna Neuer" {
		t.Fatalf("snapshot shows %q, want the updated name", got)
	}
	// An admin enable/disable notice carries no profile and changes nothing.
	h.Broadcast("user_update", map[string]any{"id": user.ID, "disabled": true})
	if got := h.voiceSnapshot()[ch.ID][user.ID].DisplayName; got != "Anna Neuer" {
		t.Fatalf("snapshot shows %q after a disable notice", got)
	}
}

func TestTypingThrottleIsPerChannel(t *testing.T) {
	c := &Client{}
	a, b := uuid.New(), uuid.New()
	now := time.Now()
	if !c.allowTyping(a, now) || !c.allowTyping(b, now) {
		t.Fatal("first notice per channel must pass")
	}
	if c.allowTyping(a, now.Add(time.Second)) || c.allowTyping(b, now.Add(time.Second)) {
		t.Fatal("alternating channels must not bypass the throttle")
	}
	// The memory is bounded.
	for i := 0; i < 3*maxTypingChannels; i++ {
		c.allowTyping(uuid.New(), now.Add(time.Second))
	}
	if len(c.typing) > maxTypingChannels {
		t.Fatalf("typing memory grew to %d", len(c.typing))
	}
}

// Speaking indicators go to the room only, only on change, and are capped.
func TestSpeakingGoesToRoomOnlyOnChange(t *testing.T) {
	h := NewHub(nil, nil, nil, nil)
	ch := voiceCh()
	speaker := testClient(h, auth.User{ID: uuid.New(), Username: "s"})
	listener := testClient(h, auth.User{ID: uuid.New(), Username: "l"})
	outsider := testClient(h, auth.User{ID: uuid.New(), Username: "o"})
	h.joinVoice(speaker, ch)
	h.joinVoice(listener, ch)
	for _, c := range []*Client{speaker, listener, outsider} {
		drainTypes(t, c)
	}

	speaker.handle("voice_speaking", []byte(`{"active":true}`))
	speaker.handle("voice_speaking", []byte(`{"active":true}`))
	if n := count(drainTypes(t, listener), "voice_speaking"); n != 1 {
		t.Fatalf("listener got %d speaking events, want 1 (only changes)", n)
	}
	if n := count(drainTypes(t, outsider), "voice_speaking"); n != 0 {
		t.Fatalf("outsider got %d speaking events, want none", n)
	}

	for i := 0; i < 100; i++ {
		speaker.handle("voice_speaking", []byte(`{"active":false}`))
		speaker.handle("voice_speaking", []byte(`{"active":true}`))
	}
	if n := count(drainTypes(t, listener), "voice_speaking"); n > 2*maxSpeakingChangesPerSec+2 {
		t.Fatalf("listener got %d speaking events from a flood", n)
	}
}

func TestDiagnosticsAreRateLimited(t *testing.T) {
	c := &Client{}
	now := time.Now()
	allowed := 0
	for i := 0; i < 50; i++ {
		if c.allowDiag(now) {
			allowed++
		}
	}
	if allowed != diagPerWindow {
		t.Fatalf("allowed %d diagnostics, want %d", allowed, diagPerWindow)
	}
	if !c.allowDiag(now.Add(diagWindow)) {
		t.Fatal("a new window must allow diagnostics again")
	}
}

// Deleting a voice channel empties its room: members are kicked and told,
// lingering presence ends.
func TestCloseVoiceChannelEvictsEveryone(t *testing.T) {
	h := NewHub(nil, nil, nil, nil)
	h.VoiceGrace = time.Hour
	ch, other := voiceCh(), voiceCh()
	member := testClient(h, auth.User{ID: uuid.New(), Username: "m"})
	elsewhere := testClient(h, auth.User{ID: uuid.New(), Username: "e"})
	dropped := testClient(h, auth.User{ID: uuid.New(), Username: "d"})
	h.joinVoice(member, ch)
	h.joinVoice(elsewhere, other)
	h.joinVoice(dropped, ch)
	h.unregister(dropped) // lingers in the room for the grace period
	if !inRoom(h, ch.ID, dropped.User.ID) {
		t.Fatal("setup: dropped user should linger")
	}
	drainTypes(t, member)

	h.CloseVoiceChannel(ch.ID)

	if member.currentVoice() != nil {
		t.Fatal("member still in the deleted room")
	}
	if n := count(drainTypes(t, member), "voice_kicked"); n != 1 {
		t.Fatalf("member got %d voice_kicked, want 1", n)
	}
	if inRoom(h, ch.ID, member.User.ID) || inRoom(h, ch.ID, dropped.User.ID) {
		t.Fatal("presence left in the deleted room")
	}
	h.mu.RLock()
	graces := len(h.grace)
	h.mu.RUnlock()
	if graces != 0 {
		t.Fatalf("%d grace timers left for the deleted room", graces)
	}
	if !inRoom(h, other.ID, elsewhere.User.ID) || elsewhere.currentVoice() == nil {
		t.Fatal("a member of another room was affected")
	}
}

// A room full by its channel's user_limit turns away a newcomer with voice_kicked (reason room_full);
// a member already present, e.g. in a second tab, still gets in.
func TestFullVoiceRoomRefusesNewcomers(t *testing.T) {
	h := NewHub(nil, nil, nil, nil)
	ch := voiceCh()
	ch.UserLimit = 2
	a := testClient(h, auth.User{ID: uuid.New(), Username: "a"})
	b := testClient(h, auth.User{ID: uuid.New(), Username: "b"})
	h.joinVoice(a, ch)
	h.joinVoice(b, ch)

	late := testClient(h, auth.User{ID: uuid.New(), Username: "late"})
	drainTypes(t, late)
	h.joinVoice(late, ch)
	if inRoom(h, ch.ID, late.User.ID) || late.currentVoice() != nil {
		t.Fatal("newcomer joined a full room")
	}
	var ev struct {
		Type    string `json:"type"`
		Payload struct {
			ChannelID uuid.UUID `json:"channel_id"`
			Reason    string    `json:"reason"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(<-late.send, &ev); err != nil {
		t.Fatal(err)
	}
	if ev.Type != "voice_kicked" || ev.Payload.ChannelID != ch.ID || ev.Payload.Reason != "room_full" {
		t.Fatalf("got %s %+v, want voice_kicked room_full", ev.Type, ev.Payload)
	}

	secondTab := testClient(h, a.User)
	h.joinVoice(secondTab, ch)
	if secondTab.currentVoice() == nil {
		t.Fatal("a member's second connection was turned away")
	}
}
