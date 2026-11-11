package ws

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// logCapture collects the server log as JSON lines while a test runs.
type logCapture struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *logCapture) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func captureLog(t *testing.T) *logCapture {
	t.Helper()
	l := &logCapture{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(l, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return l
}

// connIDs lists the conn_id of every logged line with the message msg, in
// order; a line without one counts as "".
func (l *logCapture) connIDs(msg string) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []string
	for _, line := range strings.Split(l.buf.String(), "\n") {
		var rec struct {
			Msg    string `json:"msg"`
			ConnID string `json:"conn_id"`
		}
		if json.Unmarshal([]byte(line), &rec) == nil && rec.Msg == msg {
			out = append(out, rec.ConnID)
		}
	}
	return out
}

// waitLogged waits until the lines with the message msg carry exactly the
// connection ids want, in order.
func (l *logCapture) waitLogged(t *testing.T, msg string, want ...string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		got := l.connIDs(msg)
		if strings.Join(got, ",") == strings.Join(want, ",") {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%q logged with conn_id %q, want %q", msg, got, want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// dialTab connects alice with the request id a reverse proxy would have
// given the upgrade request.
func dialTab(t *testing.T, srv *httptest.Server, requestID string) *websocket.Conn {
	t.Helper()
	header := http.Header{"Origin": []string{srv.URL}, "X-Test-User": []string{"alice"}, "X-Request-Id": []string{requestID}}
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), header)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	readUntil(t, conn, events.VoiceRooms)
	return conn
}

func clientByConnID(t *testing.T, h *Hub, id string) *Client {
	t.Helper()
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.clients {
		if c.connID == id {
			return c
		}
	}
	t.Fatalf("no connection has the id %q", id)
	return nil
}

// Two sessions of one member (two tabs) are told apart in the log: each
// one's connect, voice join, voice leave, recovered panic and disconnect
// carries the id of its own connection, which is the request id of its
// upgrade request.
func TestTwoSessionsOfOneMemberAreToldApartInTheLog(t *testing.T) {
	logged := captureLog(t)
	alice := auth.User{ID: uuid.New(), Username: "alice"}
	h := NewHub(nil, headerAuth{"alice": alice}, nil, nil, openPerms())
	h.VoiceGrace = 20 * time.Millisecond
	h.Tap = func(_ uuid.UUID, eventType string, _ any) {
		if eventType == events.Typing {
			panic("handler bug")
		}
	}
	srv := httptest.NewServer(httpx.RequestID(http.HandlerFunc(h.HandleWebSocket)))
	t.Cleanup(srv.Close)
	t.Cleanup(h.Close)
	h.Origins = []string{srv.URL}

	dialTab(t, srv, "tab-one")
	tabTwo := dialTab(t, srv, "tab-two")
	logged.waitLogged(t, "ws connected", "tab-one", "tab-two")
	one, two := clientByConnID(t, h, "tab-one"), clientByConnID(t, h, "tab-two")

	ch := voiceCh()
	for _, c := range []*Client{one, two} {
		h.joinVoice(c, ch)
		h.leaveCurrentVoice(c)
	}
	logged.waitLogged(t, "voice join", "tab-one", "tab-two")
	logged.waitLogged(t, "voice leave", "tab-one", "tab-two")

	// The second tab is in the Talk when its handler panics: the panic, the
	// disconnect and the leave its grace period ends in are the second tab's.
	h.joinVoice(two, ch)
	frame, _ := json.Marshal(map[string]any{"type": events.ClientTyping, "payload": map[string]any{"channel_id": openChannel()}})
	if err := tabTwo.WriteMessage(websocket.TextMessage, frame); err != nil {
		t.Fatal(err)
	}
	waitClosed(t, tabTwo)
	logged.waitLogged(t, "goroutine panicked", "tab-two")
	logged.waitLogged(t, "ws disconnected", "tab-two")
	logged.waitLogged(t, "voice leave", "tab-one", "tab-two", "tab-two")
}

// A connection without an upgrade request id (no request-id middleware in
// front of the hub) still gets an id of its own.
func TestConnectionWithoutARequestIDGetsAnIDOfItsOwn(t *testing.T) {
	a := newClient(nil, nil, auth.User{}, 0)
	b := newClient(nil, nil, auth.User{}, 0)
	if a.connID == "" || a.connID == b.connID {
		t.Fatalf("connection ids %q and %q, want two different ones", a.connID, b.connID)
	}
}

// The id is for the server log only: no event tells a client its own or
// anybody else's.
func TestConnectionIDIsInNoEvent(t *testing.T) {
	logged := captureLog(t)
	alice := auth.User{ID: uuid.New(), Username: "alice"}
	h := NewHub(nil, headerAuth{"alice": alice}, nil, nil, openPerms())
	srv := httptest.NewServer(httpx.RequestID(http.HandlerFunc(h.HandleWebSocket)))
	t.Cleanup(srv.Close)
	t.Cleanup(h.Close)
	h.Origins = []string{srv.URL}
	var mu sync.Mutex
	var broadcasts [][]byte
	h.Tap = func(_ uuid.UUID, eventType string, payload any) {
		data, _ := json.Marshal(map[string]any{"type": eventType, "payload": payload})
		mu.Lock()
		broadcasts = append(broadcasts, data)
		mu.Unlock()
	}

	header := http.Header{"Origin": []string{srv.URL}, "X-Test-User": []string{"alice"}, "X-Request-Id": []string{"secret-tab"}}
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), header)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	logged.waitLogged(t, "ws connected", "secret-tab")
	h.joinVoice(clientByConnID(t, h, "secret-tab"), voiceCh())

	// Everything the connection itself is sent, up to the join's answer.
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("waiting for the voice join: %v", err)
		}
		if bytes.Contains(data, []byte("secret-tab")) {
			t.Fatalf("an event carries the connection id: %s", data)
		}
		if bytes.Contains(data, []byte(`"`+events.VoiceStateUpdate+`"`)) {
			break
		}
	}
	mu.Lock()
	defer mu.Unlock()
	for _, data := range broadcasts {
		if bytes.Contains(data, []byte("secret-tab")) {
			t.Fatalf("a broadcast carries the connection id: %s", data)
		}
	}
}
