//go:build integration

package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

type wsEvent struct {
	Type    string
	Payload json.RawMessage
}

// wsConn reads in the background: a gorilla connection is unusable after a
// read deadline expires, so timeouts are handled on the channel instead.
type wsConn struct {
	t      *testing.T
	conn   *websocket.Conn
	events chan wsEvent
}

func (c *client) dialWS(origin string) (*wsConn, *http.Response, error) {
	u := "ws" + strings.TrimPrefix(c.a.srv.URL, "http") + "/api/ws"
	hdr := http.Header{}
	if origin != "" {
		hdr.Set("Origin", origin)
	}
	for _, ck := range c.http.Jar.Cookies(mustURL(c.a.srv.URL)) {
		hdr.Add("Cookie", ck.Name+"="+ck.Value)
	}
	conn, res, err := websocket.DefaultDialer.Dial(u, hdr)
	if err != nil {
		return nil, res, err
	}
	c.a.t.Cleanup(func() { conn.Close() })
	w := &wsConn{t: c.a.t, conn: conn, events: make(chan wsEvent, 64)}
	go func() {
		defer close(w.events)
		for {
			var ev wsEvent
			if err := conn.ReadJSON(&ev); err != nil {
				return
			}
			w.events <- ev
		}
	}()
	return w, res, nil
}

func (w *wsConn) send(eventType string, payload any) {
	w.t.Helper()
	if err := w.conn.WriteJSON(map[string]any{"type": eventType, "payload": payload}); err != nil {
		w.t.Fatal(err)
	}
}

// expect waits for an event of the given type; ok=false on timeout.
func (w *wsConn) expect(eventType string, within time.Duration) (json.RawMessage, bool) {
	timeout := time.After(within)
	for {
		select {
		case ev, ok := <-w.events:
			if !ok {
				return nil, false
			}
			if ev.Type == eventType {
				return ev.Payload, true
			}
		case <-timeout:
			return nil, false
		}
	}
}

func TestWebSocketRequiresSessionAndOrigin(t *testing.T) {
	a := newApp(t, true)
	admin := a.seedAdmin()

	if _, res, err := a.anon().dialWS(a.origin()); err == nil || res == nil || res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous websocket: err=%v res=%v", err, res)
	}
	if _, res, err := admin.dialWS("https://evil.example"); err == nil || res == nil || res.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-site websocket: err=%v res=%v", err, res)
	}
	if _, _, err := admin.dialWS(a.origin()); err != nil {
		t.Fatalf("same-origin websocket: %v", err)
	}
}

func TestVoiceJoinOnlyForVoiceChannels(t *testing.T) {
	a := newApp(t, true)
	admin := a.seedAdmin()
	max := a.register(admin, "max")
	text := a.createChannel(admin, "general", "text")
	voice := a.createChannel(admin, "Lounge", "voice")

	watcher, _, err := admin.dialWS(a.origin())
	if err != nil {
		t.Fatal(err)
	}
	joiner, _, err := max.dialWS(a.origin())
	if err != nil {
		t.Fatal(err)
	}

	joiner.send("voice_join", map[string]any{"channel_id": text})
	joiner.send("voice_join", map[string]any{"channel_id": uuid.New()})
	if _, got := watcher.expect("voice_state_update", 500*time.Millisecond); got {
		t.Fatal("joining a text or unknown channel must not create voice presence")
	}

	joiner.send("voice_join", map[string]any{"channel_id": voice})
	payload, got := watcher.expect("voice_state_update", 2*time.Second)
	if !got || !strings.Contains(string(payload), voice.String()) || !strings.Contains(string(payload), `"join"`) {
		t.Fatalf("voice join not announced: %s", payload)
	}

	joiner.send("voice_leave", nil)
	payload, got = watcher.expect("voice_state_update", 2*time.Second)
	if !got || !strings.Contains(string(payload), `"leave"`) {
		t.Fatalf("voice leave not announced: %s", payload)
	}
}

// TestVoiceSurvivesReconnectWithinGrace models a page reload during a call:
// the others see no leave/join flicker, and only a real absence ends presence.
func TestVoiceSurvivesReconnectWithinGrace(t *testing.T) {
	a := newApp(t, true)
	a.router.Hub.VoiceGrace = 400 * time.Millisecond
	admin := a.seedAdmin()
	max := a.register(admin, "max")
	voice := a.createChannel(admin, "Lounge", "voice")

	watcher, _, err := admin.dialWS(a.origin())
	if err != nil {
		t.Fatal(err)
	}
	first, _, err := max.dialWS(a.origin())
	if err != nil {
		t.Fatal(err)
	}
	first.send("voice_join", map[string]any{"channel_id": voice})
	if _, ok := watcher.expect("voice_state_update", 2*time.Second); !ok {
		t.Fatal("initial join not announced")
	}

	// "Reload": drop the socket and come back within the grace period.
	first.conn.Close()
	time.Sleep(100 * time.Millisecond)
	second, _, err := max.dialWS(a.origin())
	if err != nil {
		t.Fatal(err)
	}
	snapshot, ok := second.expect("voice_snapshot", 2*time.Second)
	if !ok || !strings.Contains(string(snapshot), max.user.ID.String()) {
		t.Fatalf("reconnecting client must still see itself in the room: %s", snapshot)
	}
	second.send("voice_join", map[string]any{"channel_id": voice})
	if payload, flicker := watcher.expect("voice_state_update", 700*time.Millisecond); flicker {
		t.Fatalf("others saw a leave/join flicker on reload: %s", payload)
	}

	// A real disconnect ends presence after the grace period.
	second.conn.Close()
	payload, ok := watcher.expect("voice_state_update", 2*time.Second)
	if !ok || !strings.Contains(string(payload), `"leave"`) {
		t.Fatalf("leave after grace not announced: %s", payload)
	}
}

// TestVoiceSurvivesLateUnregisterOfOldSocket models a reconnect whose new
// socket is back in the call before the old one unregisters: the old socket
// must not end the new connection's voice presence.
func TestVoiceSurvivesLateUnregisterOfOldSocket(t *testing.T) {
	a := newApp(t, true)
	a.router.Hub.VoiceGrace = 300 * time.Millisecond
	admin := a.seedAdmin()
	max := a.register(admin, "max")
	voice := a.createChannel(admin, "Lounge", "voice")

	watcher, _, err := admin.dialWS(a.origin())
	if err != nil {
		t.Fatal(err)
	}
	old, _, err := max.dialWS(a.origin())
	if err != nil {
		t.Fatal(err)
	}
	old.send("voice_join", map[string]any{"channel_id": voice})
	if _, ok := watcher.expect("voice_state_update", 2*time.Second); !ok {
		t.Fatal("initial join not announced")
	}

	fresh, _, err := max.dialWS(a.origin())
	if err != nil {
		t.Fatal(err)
	}
	fresh.send("voice_join", map[string]any{"channel_id": voice})
	time.Sleep(200 * time.Millisecond)
	old.conn.Close()

	if payload, left := watcher.expect("voice_state_update", 900*time.Millisecond); left {
		t.Fatalf("closing the old socket ended the new connection's voice: %s", payload)
	}
}

func TestTypingIsRelayedToOthersAndThrottled(t *testing.T) {
	a := newApp(t, true)
	admin := a.seedAdmin()
	max := a.register(admin, "max")
	text := a.createChannel(admin, "allgemein", "text")
	voice := a.createChannel(admin, "Lounge", "voice")

	watcher, _, err := admin.dialWS(a.origin())
	if err != nil {
		t.Fatal(err)
	}
	typer, _, err := max.dialWS(a.origin())
	if err != nil {
		t.Fatal(err)
	}

	typer.send("typing", map[string]any{"channel_id": voice})
	typer.send("typing", map[string]any{"channel_id": uuid.New()})
	if payload, got := watcher.expect("typing", 400*time.Millisecond); got {
		t.Fatalf("typing in a voice or unknown channel relayed: %s", payload)
	}

	typer.send("typing", map[string]any{"channel_id": text})
	payload, got := watcher.expect("typing", 2*time.Second)
	if !got || !strings.Contains(string(payload), text.String()) || !strings.Contains(string(payload), max.user.ID.String()) {
		t.Fatalf("typing not relayed: %s", payload)
	}
	if _, echoed := typer.expect("typing", 300*time.Millisecond); echoed {
		t.Fatal("typing echoed back to the typer")
	}

	// A burst within the throttle window is relayed once.
	typer.send("typing", map[string]any{"channel_id": text})
	typer.send("typing", map[string]any{"channel_id": text})
	if payload, again := watcher.expect("typing", 500*time.Millisecond); again {
		t.Fatalf("typing burst not throttled: %s", payload)
	}
}
