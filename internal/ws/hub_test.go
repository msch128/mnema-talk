package ws

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/sfu"
)

func isUserInSlice(users []uuid.UUID, id uuid.UUID) bool {
	for _, u := range users {
		if u == id {
			return true
		}
	}
	return false
}

func TestHubClientRegistrationAndPresence(t *testing.T) {
	h := NewHub(nil, nil, nil, nil)
	user := auth.User{
		ID:          uuid.New(),
		Username:    "alice",
		DisplayName: "Alice",
	}

	if isUserInSlice(h.onlineUsers(), user.ID) {
		t.Fatalf("expected user to be offline initially")
	}

	c := &Client{
		hub:  h,
		User: user,
		send: make(chan []byte, 16),
	}

	h.register(c)
	if !isUserInSlice(h.onlineUsers(), user.ID) {
		t.Fatalf("expected user to be online after register")
	}

	// Another connection for the same user
	c2 := &Client{
		hub:  h,
		User: user,
		send: make(chan []byte, 16),
	}
	h.register(c2)
	if !isUserInSlice(h.onlineUsers(), user.ID) {
		t.Fatalf("expected user to remain online")
	}

	h.unregister(c)
	if !isUserInSlice(h.onlineUsers(), user.ID) {
		t.Fatalf("expected user to still be online with second client")
	}

	h.unregister(c2)
	if isUserInSlice(h.onlineUsers(), user.ID) {
		t.Fatalf("expected user to be offline after all clients unregister")
	}
}

func TestHubBroadcastAndSendToUsers(t *testing.T) {
	h := NewHub(nil, nil, nil, nil)
	u1 := auth.User{ID: uuid.New(), Username: "u1"}
	u2 := auth.User{ID: uuid.New(), Username: "u2"}

	c1 := &Client{hub: h, User: u1, send: make(chan []byte, 16)}
	c2 := &Client{hub: h, User: u2, send: make(chan []byte, 16)}

	h.register(c1)
	h.register(c2)
	defer h.unregister(c1)
	defer h.unregister(c2)

	// Drain initial presence events
drain:
	for {
		select {
		case <-c1.send:
		case <-c2.send:
		default:
			break drain
		}
	}

	// Broadcast
	h.Broadcast("test_event", map[string]string{"greeting": "hello"})

	select {
	case msg := <-c1.send:
		var ev Event
		if err := json.Unmarshal(msg, &ev); err != nil || ev.Type != "test_event" {
			t.Fatalf("unexpected message on c1: %s", string(msg))
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatalf("timeout waiting for broadcast on c1")
	}

	select {
	case msg := <-c2.send:
		var ev Event
		if err := json.Unmarshal(msg, &ev); err != nil || ev.Type != "test_event" {
			t.Fatalf("unexpected message on c2: %s", string(msg))
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatalf("timeout waiting for broadcast on c2")
	}

	// Send to specific user
	h.SendToUsers([]uuid.UUID{u1.ID}, "direct_event", "secret")
	select {
	case msg := <-c1.send:
		var ev Event
		if err := json.Unmarshal(msg, &ev); err != nil || ev.Type != "direct_event" {
			t.Fatalf("unexpected direct message on c1: %s", string(msg))
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatalf("timeout waiting for direct message on c1")
	}

	// c2 should not have received direct_event
	select {
	case msg := <-c2.send:
		t.Fatalf("c2 unexpectedly received message: %s", string(msg))
	default:
	}
}

func TestHubTypingThrottling(t *testing.T) {
	c := &Client{
		User: auth.User{ID: uuid.New(), Username: "u"},
		send: make(chan []byte, 16),
	}

	chID := uuid.New()
	now := time.Now()

	// First typing is allowed
	if !c.allowTyping(chID, now) {
		t.Fatalf("first typing should be allowed")
	}

	// Immediate next typing within 3s is throttled
	if c.allowTyping(chID, now.Add(1*time.Second)) {
		t.Fatalf("typing within 3s should be throttled")
	}

	// Typing after 3.1s is allowed
	if !c.allowTyping(chID, now.Add(3100*time.Millisecond)) {
		t.Fatalf("typing after 3.1s should be allowed")
	}
}

func TestHubVoiceSnapshotAndSpeaking(t *testing.T) {
	h := NewHub(nil, nil, nil, nil)
	chID := uuid.New()
	user := auth.User{ID: uuid.New(), Username: "speaker"}

	c := &Client{
		hub:  h,
		User: user,
		send: make(chan []byte, 16),
	}

	h.register(c)
	defer h.unregister(c)

	// Simulate voice join directly in state
	h.mu.Lock()
	if h.voice[chID] == nil {
		h.voice[chID] = make(map[uuid.UUID]auth.User)
	}
	h.voice[chID][user.ID] = user
	c.voiceCh = &chID
	h.mu.Unlock()

	snap := h.voiceSnapshot()
	if len(snap[chID]) != 1 || snap[chID][user.ID].ID != user.ID {
		t.Fatalf("expected voice snapshot to include user in room, got: %+v", snap)
	}

	// Drain
drain:
	for {
		select {
		case <-c.send:
		default:
			break drain
		}
	}

	// Speaking event dispatch
	c.handle("voice_speaking", []byte(`{"active":true}`))

	select {
	case msg := <-c.send:
		var ev Event
		if err := json.Unmarshal(msg, &ev); err != nil || ev.Type != "voice_speaking" {
			t.Fatalf("expected voice_speaking event, got: %s", string(msg))
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatalf("timeout waiting for voice_speaking event")
	}
}

func TestHubVoiceGracePeriod(t *testing.T) {
	h := NewHub(nil, nil, nil, nil)
	h.VoiceGrace = 50 * time.Millisecond
	chID := uuid.New()
	user := auth.User{ID: uuid.New(), Username: "reloader"}

	c := &Client{
		hub:  h,
		User: user,
		send: make(chan []byte, 16),
	}

	h.register(c)
	h.mu.Lock()
	if h.voice[chID] == nil {
		h.voice[chID] = make(map[uuid.UUID]auth.User)
	}
	h.voice[chID][user.ID] = user
	c.voiceCh = &chID
	h.mu.Unlock()

	// When client unregisters, it starts grace period instead of immediately leaving
	h.unregister(c)

	h.mu.RLock()
	inRoomDuringGrace := h.voice[chID][user.ID].ID == user.ID
	h.mu.RUnlock()
	if !inRoomDuringGrace {
		t.Fatalf("user should remain in room during grace period")
	}

	// Wait for grace period to expire
	time.Sleep(70 * time.Millisecond)

	h.mu.RLock()
	inRoomAfterGrace := false
	if m, ok := h.voice[chID]; ok {
		_, inRoomAfterGrace = m[user.ID]
	}
	h.mu.RUnlock()
	if inRoomAfterGrace {
		t.Fatalf("user should be removed from room after grace period expires")
	}
}

func TestHubSubscribeValidation(t *testing.T) {
	voiceSFU, err := sfu.NewSFU(0, 0, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	h := NewHub(nil, nil, voiceSFU, nil)
	ch := uuid.New()
	viewer := &Client{hub: h, User: auth.User{ID: uuid.New()}, send: make(chan []byte, 16)}
	pub := uuid.New()
	if _, _, err := voiceSFU.Join(ch, viewer.User.ID, nil, nil); err != nil {
		t.Fatal(err)
	}
	room := voiceSFU.Room(ch)

	sub := func(body string) { viewer.handle("webrtc_subscribe", []byte(body)) }
	id := pub.String()

	// Not in a voice room: ignored.
	sub(`{"kind":"screen","user_id":"` + id + `","on":true}`)
	if room.Receives(viewer.User.ID, pub, sfu.SourceScreen) {
		t.Fatal("subscription from outside a room must be ignored")
	}

	viewer.setVoice(&ch)
	sub(`{"kind":"screen","user_id":"` + id + `","on":true}`)
	if !room.Receives(viewer.User.ID, pub, sfu.SourceScreen) {
		t.Fatal("valid screen subscription not applied")
	}
	sub(`{"kind":"screen","user_id":"` + id + `","on":false}`)
	if room.Receives(viewer.User.ID, pub, sfu.SourceScreen) {
		t.Fatal("unsubscribe not applied")
	}
	sub(`{"kind":"camera","user_id":"` + id + `","on":false}`)
	if room.Receives(viewer.User.ID, pub, sfu.SourceCamera) {
		t.Fatal("camera opt-out not applied")
	}
	sub(`{"kind":"camera","user_id":"` + id + `","on":true}`)
	if !room.Receives(viewer.User.ID, pub, sfu.SourceCamera) {
		t.Fatal("camera opt-in not applied")
	}
	sub(`{"kind":"camera","all":true,"on":false}`)
	if room.Receives(viewer.User.ID, pub, sfu.SourceCamera) {
		t.Fatal("all cameras off not applied")
	}

	// Invalid input changes nothing.
	for _, bad := range []string{
		`{"kind":"audio","user_id":"` + id + `","on":false}`,
		`{"kind":"screen","user_id":"not-a-uuid","on":true}`,
		`{"kind":"screen","all":true,"on":true}`,
		`{"kind":"screen","user_id":"` + viewer.User.ID.String() + `","on":true}`,
		`{"kind":"screen","user_id":"` + uuid.Nil.String() + `","on":true}`,
		`not json`,
	} {
		sub(bad)
	}
	if room.Receives(viewer.User.ID, pub, sfu.SourceScreen) {
		t.Fatal("invalid subscribe must be ignored")
	}
}

func TestHubUserUpdateRefreshesVoiceUsers(t *testing.T) {
	h := NewHub(nil, nil, nil, nil)
	chID := uuid.New()
	user := auth.User{ID: uuid.New(), Username: "painter", DisplayName: "Old"}
	other := auth.User{ID: uuid.New(), Username: "bystander", DisplayName: "Bystander"}

	c := &Client{hub: h, User: user, send: make(chan []byte, 16)}
	o := &Client{hub: h, User: other, send: make(chan []byte, 16)}
	h.register(c)
	defer h.unregister(c)
	h.register(o)
	defer h.unregister(o)

	h.mu.Lock()
	h.voice[chID] = map[uuid.UUID]auth.User{user.ID: user, other.ID: other}
	h.mu.Unlock()

	changed := user
	changed.DisplayName = "New"
	changed.AvatarURL = "/api/media/" + uuid.NewString()
	changed.StatusText = "painting"
	h.Broadcast("user_update", changed.Public())

	snap := h.voiceSnapshot()
	got := snap[chID][user.ID]
	if got.DisplayName != "New" || got.AvatarURL != changed.AvatarURL || got.StatusText != "painting" {
		t.Fatalf("voice snapshot kept the old profile: %+v", got.User)
	}
	if snap[chID][other.ID].DisplayName != "Bystander" {
		t.Fatalf("other users must stay untouched: %+v", snap[chID][other.ID].User)
	}

	h.mu.RLock()
	connUser := c.User
	h.mu.RUnlock()
	if connUser.AvatarURL != changed.AvatarURL || connUser.Username != "painter" {
		t.Fatalf("connection keeps a stale profile for later voice joins: %+v", connUser)
	}

	// An admin's disable notice is not a profile and changes nothing.
	h.Broadcast("user_update", map[string]any{"id": user.ID, "disabled": true})
	if h.voiceSnapshot()[chID][user.ID].DisplayName != "New" {
		t.Fatal("disable notice must not reset the profile")
	}
}
