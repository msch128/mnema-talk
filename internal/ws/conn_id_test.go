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
	"github.com/msch128/mnema-talk/internal/safego"
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

func (l *logCapture) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

func captureLog(t *testing.T) *logCapture {
	t.Helper()
	l := &logCapture{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(l, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return l
}

// attr lists the attribute key of every logged line with the message msg, in
// order; a line without it counts as "".
func (l *logCapture) attr(msg, key string) []string {
	var out []string
	for _, line := range strings.Split(l.String(), "\n") {
		rec := map[string]any{}
		if json.Unmarshal([]byte(line), &rec) == nil && rec["msg"] == msg {
			value, _ := rec[key].(string)
			out = append(out, value)
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
		got := l.attr(msg, "conn_id")
		if strings.Join(got, ",") == strings.Join(want, ",") {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%q logged with conn_id %q, want %q", msg, got, want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// tabServer serves alice's hub behind the request-id middleware and records
// the request id of every upgrade request, in order.
type tabServer struct {
	*httptest.Server
	hub *Hub
	mu  sync.Mutex
	ids []string
}

func newTabServer(t *testing.T) *tabServer {
	t.Helper()
	alice := auth.User{ID: uuid.New(), Username: "alice"}
	s := &tabServer{hub: NewHub(nil, headerAuth{"alice": alice}, nil, nil, openPerms())}
	s.Server = httptest.NewServer(httpx.RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.ids = append(s.ids, httpx.RequestIDFromContext(r.Context()))
		s.mu.Unlock()
		s.hub.HandleWebSocket(w, r)
	})))
	t.Cleanup(s.Close)
	t.Cleanup(s.hub.Close)
	s.hub.Origins = []string{s.URL}
	return s
}

func (s *tabServer) requestIDs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.ids...)
}

// dialTab connects alice and waits for her greeting. requestID is the
// X-Request-Id her client (or a proxy) sends with the upgrade request, ""
// for none.
func (s *tabServer) dialTab(t *testing.T, requestID string) *websocket.Conn {
	t.Helper()
	header := http.Header{"Origin": []string{s.URL}, "X-Test-User": []string{"alice"}}
	if requestID != "" {
		header.Set("X-Request-Id", requestID)
	}
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(s.URL, "http"), header)
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
// carries the id of its own connection, which is the request id the server
// gave its upgrade request.
func TestTwoSessionsOfOneMemberAreToldApartInTheLog(t *testing.T) {
	// The panic's line is read below: an earlier test or run may have used
	// up what the site may log in a minute.
	safego.RefillLogBudget()
	logged := captureLog(t)
	srv := newTabServer(t)
	h := srv.hub
	h.VoiceGrace = 20 * time.Millisecond
	h.Tap = func(_ uuid.UUID, eventType string, _ any) {
		if eventType == events.Typing {
			panic("handler bug")
		}
	}

	srv.dialTab(t, "")
	tabTwo := srv.dialTab(t, "")
	ids := srv.requestIDs()
	if len(ids) != 2 || ids[0] == "" || ids[0] == ids[1] {
		t.Fatalf("upgrade request ids %q, want two different ones", ids)
	}
	first, second := ids[0], ids[1]
	logged.waitLogged(t, "ws connected", first, second)
	if got := logged.attr("ws connected", "client_request_id"); strings.Join(got, "") != "" {
		t.Fatalf("a request id the server made up is logged as the client's: %q", got)
	}
	one, two := clientByConnID(t, h, first), clientByConnID(t, h, second)

	ch := voiceCh()
	for _, c := range []*Client{one, two} {
		h.joinVoice(c, ch)
		h.leaveCurrentVoice(c)
	}
	logged.waitLogged(t, "voice join", first, second)
	logged.waitLogged(t, "voice leave", first, second)

	// The second tab is in the Talk when its handler panics: the panic, the
	// disconnect and the leave its grace period ends in are the second tab's.
	h.joinVoice(two, ch)
	frame, _ := json.Marshal(map[string]any{"type": events.ClientTyping, "payload": map[string]any{"channel_id": openChannel()}})
	if err := tabTwo.WriteMessage(websocket.TextMessage, frame); err != nil {
		t.Fatal(err)
	}
	waitClosed(t, tabTwo)
	logged.waitLogged(t, "goroutine panicked", second)
	logged.waitLogged(t, "ws disconnected", second)
	logged.waitLogged(t, "voice leave", first, second, second)
}

// A request id that came with the upgrade request is the sender's choice, so
// it never names the connection: two connections that send the same one (or
// the id of somebody else's session) still get ids of their own. What they
// sent is logged once, when they connect, and on no later line.
func TestRequestIDChosenByTheClientIsNotTheConnectionID(t *testing.T) {
	logged := captureLog(t)
	srv := newTabServer(t)

	srv.dialTab(t, "tab-one")
	srv.dialTab(t, "tab-one")
	deadline := time.Now().Add(5 * time.Second)
	for len(logged.attr("ws connected", "conn_id")) != 2 {
		if time.Now().After(deadline) {
			t.Fatal("the two connections were never logged")
		}
		time.Sleep(5 * time.Millisecond)
	}
	ids := logged.attr("ws connected", "conn_id")
	if ids[0] == "" || ids[0] == ids[1] || ids[0] == "tab-one" || ids[1] == "tab-one" {
		t.Fatalf("connection ids %q, want two random ones", ids)
	}
	if got := logged.attr("ws connected", "client_request_id"); strings.Join(got, ",") != "tab-one,tab-one" {
		t.Fatalf("client request ids %q logged at connect, want tab-one twice", got)
	}

	c := clientByConnID(t, srv.hub, ids[0])
	srv.hub.joinVoice(c, voiceCh())
	srv.hub.leaveCurrentVoice(c)
	logged.waitLogged(t, "voice leave", ids[0])
	if mentions := strings.Count(logged.String(), "tab-one"); mentions != 2 {
		t.Fatalf("the client's request id is logged %d times, want once per connect", mentions)
	}
}

// A request id the middleware refuses (too long, or with characters that
// could forge a log line) is replaced by the server's own, and that one
// names the connection: nothing of what the client sent reaches the log.
func TestRefusedClientRequestIDLeavesNoTraceInTheLog(t *testing.T) {
	logged := captureLog(t)
	srv := newTabServer(t)
	for _, sent := range []string{`evil id" conn_id="other`, strings.Repeat("x", 129)} {
		srv.dialTab(t, sent)
	}
	ids := srv.requestIDs()
	logged.waitLogged(t, "ws connected", ids[0], ids[1])
	out := logged.String()
	if strings.Contains(out, "evil") || strings.Contains(out, "xxxx") || strings.Contains(out, "client_request_id") {
		t.Fatalf("the log carries what the client sent: %s", out)
	}
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
	srv := newTabServer(t)
	h := srv.hub
	var mu sync.Mutex
	var broadcasts [][]byte
	h.Tap = func(_ uuid.UUID, eventType string, payload any) {
		data, _ := json.Marshal(map[string]any{"type": eventType, "payload": payload})
		mu.Lock()
		broadcasts = append(broadcasts, data)
		mu.Unlock()
	}

	header := http.Header{"Origin": []string{srv.URL}, "X-Test-User": []string{"alice"}}
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), header)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	id := srv.requestIDs()[0]
	logged.waitLogged(t, "ws connected", id)
	h.joinVoice(clientByConnID(t, h, id), voiceCh())

	// Everything the connection itself is sent, up to the join's answer.
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("waiting for the voice join: %v", err)
		}
		if bytes.Contains(data, []byte(id)) {
			t.Fatalf("an event carries the connection id: %s", data)
		}
		if bytes.Contains(data, []byte(`"`+events.VoiceStateUpdate+`"`)) {
			break
		}
	}
	mu.Lock()
	defer mu.Unlock()
	for _, data := range broadcasts {
		if bytes.Contains(data, []byte(id)) {
			t.Fatalf("a broadcast carries the connection id: %s", data)
		}
	}
}
