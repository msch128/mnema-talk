package ws

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/metrics"
	"github.com/msch128/mnema-talk/internal/sfu"
)

// headerAuth signs a connection in as the user its X-Test-User header names.
type headerAuth map[string]auth.User

func (a headerAuth) AuthenticateRequest(r *http.Request) (*auth.User, int, error) {
	u := a[r.Header.Get("X-Test-User")]
	return &u, 0, nil
}

func dialAs(t *testing.T, srv *httptest.Server, name string) *websocket.Conn {
	t.Helper()
	header := http.Header{"Origin": []string{srv.URL}, "X-Test-User": []string{name}}
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), header)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// readUntil reads events until one of the wanted type arrives.
func readUntil(t *testing.T, conn *websocket.Conn, eventType string) {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("waiting for %s: %v", eventType, err)
		}
		var ev struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(data, &ev) == nil && ev.Type == eventType {
			return
		}
	}
}

// A handler that panics takes down its own connection and nothing else: the
// other members stay connected and keep receiving events, the hub's lock is
// free, and the panic is counted.
func TestPanickingHandlerClosesOnlyItsOwnConnection(t *testing.T) {
	alice := auth.User{ID: uuid.New(), Username: "alice"}
	bob := auth.User{ID: uuid.New(), Username: "bob"}
	h := NewHub(nil, headerAuth{"alice": alice, "bob": bob}, nil, nil, openPerms())
	ch := openChannel()
	h.Tap = func(_ uuid.UUID, eventType string, _ any) {
		if eventType == events.Typing {
			panic("handler bug")
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(h.HandleWebSocket))
	t.Cleanup(srv.Close)
	t.Cleanup(h.Close)
	h.Origins = []string{srv.URL}

	aliceConn := dialAs(t, srv, "alice")
	bobConn := dialAs(t, srv, "bob")
	readUntil(t, aliceConn, events.VoiceRooms)
	readUntil(t, bobConn, events.VoiceRooms)
	for deadline := time.Now().Add(5 * time.Second); h.OnlineCount() != 2; time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("both members never came online")
		}
	}
	before := metrics.GoroutinePanics.With("ws_read").Load()

	frame, _ := json.Marshal(map[string]any{"type": events.ClientTyping, "payload": map[string]any{"channel_id": ch}})
	if err := aliceConn.WriteMessage(websocket.TextMessage, frame); err != nil {
		t.Fatal(err)
	}

	_ = aliceConn.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		_, _, err := aliceConn.ReadMessage()
		if err == nil {
			continue
		}
		if timeout, ok := err.(interface{ Timeout() bool }); ok && timeout.Timeout() {
			t.Fatal("the panicking connection stayed open")
		}
		break
	}
	waitCounted(t, "ws_read", before+1)

	// Bob hears that alice went offline, and an event published afterwards.
	readUntil(t, bobConn, events.PresenceUpdate)
	h.Broadcast(events.MemberJoined, map[string]any{})
	readUntil(t, bobConn, events.MemberJoined)
	if got := h.OnlineCount(); got != 1 {
		t.Fatalf("%d members online, want only bob", got)
	}
}

// hubWithBob starts a hub whose Tap is tap and connects bob to it.
func hubWithBob(t *testing.T, tap func(eventType string), configure ...func(*Hub)) (h *Hub, srv *httptest.Server, bob *websocket.Conn) {
	t.Helper()
	users := headerAuth{"alice": {ID: uuid.New(), Username: "alice"}, "bob": {ID: uuid.New(), Username: "bob"}}
	h = NewHub(nil, users, nil, nil, openPerms())
	h.Tap = func(_ uuid.UUID, eventType string, _ any) { tap(eventType) }
	for _, set := range configure {
		set(h)
	}
	srv = httptest.NewServer(http.HandlerFunc(h.HandleWebSocket))
	t.Cleanup(srv.Close)
	t.Cleanup(h.Close)
	h.Origins = []string{srv.URL}
	bob = dialAs(t, srv, "bob")
	readUntil(t, bob, events.VoiceRooms)
	waitOnline(t, h, 1)
	return h, srv, bob
}

// waitCounted waits until the panics of a site reach want: the socket closes
// a moment before the panic is counted.
func waitCounted(t *testing.T, name string, want uint64) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); metrics.GoroutinePanics.With(name).Load() != want; time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("%s counted %d panics, want %d", name, metrics.GoroutinePanics.With(name).Load(), want)
		}
	}
}

func waitOnline(t *testing.T, h *Hub, want int) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); h.OnlineCount() != want; time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("%d members online, want %d", h.OnlineCount(), want)
		}
	}
}

func waitClosed(t *testing.T, conn *websocket.Conn) {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		_, _, err := conn.ReadMessage()
		if err == nil {
			continue
		}
		if timeout, ok := err.(interface{ Timeout() bool }); ok && timeout.Timeout() {
			t.Fatal("the connection stayed open")
		}
		return
	}
}

// The clean-up of a panicking connection can panic too (here: its voice
// clean-up). The client still leaves the hub and its socket is closed,
// instead of staying listed as online with nobody reading it.
func TestPanicWhileUnregisteringStillRemovesTheClient(t *testing.T) {
	h, srv, bob := hubWithBob(t, func(eventType string) {
		if eventType == events.Typing {
			panic("handler bug")
		}
	})
	alice := dialAs(t, srv, "alice")
	readUntil(t, alice, events.VoiceRooms)
	waitOnline(t, h, 2)
	ch := openChannel()
	var user uuid.UUID
	h.mu.Lock()
	for c := range h.clients {
		if c.User.Username == "alice" {
			user = c.User.ID
			c.setVoice(&ch) // seated, so unregistering starts a grace leave ...
			h.voice[ch] = map[uuid.UUID]auth.User{user: c.User}
		}
	}
	h.grace = nil // ... which panics on the missing map
	h.mu.Unlock()

	frame, _ := json.Marshal(map[string]any{"type": events.ClientTyping, "payload": map[string]any{"channel_id": ch}})
	if err := alice.WriteMessage(websocket.TextMessage, frame); err != nil {
		t.Fatal(err)
	}

	waitClosed(t, alice)
	waitOnline(t, h, 1)
	// No grace leave could be started, so the seat is given up at once.
	for deadline := time.Now().Add(5 * time.Second); inRoom(h, ch, user); time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the member stayed listed in the Talk with no connection and no grace leave")
		}
	}
	h.Broadcast(events.MemberJoined, map[string]any{})
	readUntil(t, bob, events.MemberJoined)
}

// A member who sits in a Talk when a handler of theirs panics leaves it like
// after any lost connection: the media peer goes at once, the seat after the
// grace time, the others are told and the stay's time accounting ends.
func TestPanickingHandlerOfASeatedMemberFreesTheSeat(t *testing.T) {
	left := make(chan uuid.UUID, 1)
	media, err := sfu.NewSFU(0, 0, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = media.Close() })
	h, srv, _ := hubWithBob(t, func(eventType string) {
		if eventType == events.Typing {
			panic("handler bug")
		}
	}, func(h *Hub) {
		h.SFU = media
		h.VoiceGrace = 100 * time.Millisecond
		h.Presence = func(user, _ uuid.UUID, joined bool) {
			if !joined {
				left <- user
			}
		}
	})
	alice := dialAs(t, srv, "alice")
	readUntil(t, alice, events.VoiceRooms)
	waitOnline(t, h, 2)
	var c *Client
	h.mu.RLock()
	for candidate := range h.clients {
		if candidate.User.Username == "alice" {
			c = candidate
		}
	}
	h.mu.RUnlock()
	ch := voiceCh()
	h.joinVoice(c, ch)
	key := voiceKey{c.User.ID, ch.ID}
	if !inRoom(h, ch.ID, c.User.ID) || media.Room(ch.ID) == nil {
		t.Fatal("the member did not get a seat and a media peer")
	}

	frame, _ := json.Marshal(map[string]any{"type": events.ClientTyping, "payload": map[string]any{"channel_id": openChannel()}})
	if err := alice.WriteMessage(websocket.TextMessage, frame); err != nil {
		t.Fatal(err)
	}

	waitClosed(t, alice)
	select {
	case user := <-left:
		if user != c.User.ID {
			t.Fatalf("%s left, want the panicking member", user)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the others were never told that the member left the Talk")
	}
	h.mu.RLock()
	_, counting := h.voiceSince[key]
	_, graceLeft := h.grace[key]
	h.mu.RUnlock()
	if inRoom(h, ch.ID, c.User.ID) || counting || graceLeft || media.Room(ch.ID) != nil || c.currentVoice() != nil {
		t.Fatalf("after the grace time: listed=%t, time counting=%t, grace pending=%t, media room left=%t",
			inRoom(h, ch.ID, c.User.ID), counting, graceLeft, media.Room(ch.ID) != nil)
	}
}

// A panic in the greeting happens after the upgrade, where the HTTP server
// can no longer answer. The half-greeted connection is closed and leaves the
// hub, and the panic is counted.
func TestPanicInTheGreetingClosesThatConnection(t *testing.T) {
	var armed atomic.Bool
	h, srv, bob := hubWithBob(t, func(eventType string) {
		// Armed once bob is in: the next presence update is alice coming
		// online, in her greeting. Her going offline afterwards is not hit.
		if eventType == events.PresenceUpdate && armed.CompareAndSwap(true, false) {
			panic("greeting bug")
		}
	})
	before := metrics.GoroutinePanics.With("ws_handshake").Load()
	armed.Store(true)
	alice := dialAs(t, srv, "alice")
	waitClosed(t, alice)
	waitOnline(t, h, 1)
	waitCounted(t, "ws_handshake", before+1)
	h.Broadcast(events.MemberJoined, map[string]any{})
	readUntil(t, bob, events.MemberJoined)
}
