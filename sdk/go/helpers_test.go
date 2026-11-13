package mnema

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

const testToken = "mnb_test"

// fakeGateway is an httptest server that speaks the gateway protocol under
// the test's control: each upgrade sends HELLO and hands the connection to
// the test through conns.
type fakeGateway struct {
	srv       *httptest.Server
	conns     chan *fakeConn
	interval  int // heartbeat interval in ms, sent in HELLO
	transport *http.Transport
	mu        sync.Mutex
	rest      http.HandlerFunc // other routes, optional
}

func newFakeGateway(t *testing.T, intervalMS int) *fakeGateway {
	t.Helper()
	g := &fakeGateway{conns: make(chan *fakeConn, 8), interval: intervalMS, transport: &http.Transport{}}
	up := websocket.Upgrader{}
	g.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == BasePath+"/gateway" && websocket.IsWebSocketUpgrade(r) {
			ws, err := up.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			fc := &fakeConn{ws: ws, header: r.Header.Clone(), frames: make(chan frame, 64)}
			fc.write(t, opHello, map[string]int{"heartbeat_interval": g.interval})
			go fc.readLoop()
			g.conns <- fc
			return
		}
		if r.URL.Path == BasePath+"/gateway" {
			writeJSON(w, http.StatusOK, GatewayInfo{URL: "ws" + strings.TrimPrefix(g.srv.URL, "http") + BasePath + "/gateway", HeartbeatInterval: g.interval, MaxConnections: 3})
			return
		}
		g.mu.Lock()
		h := g.rest
		g.mu.Unlock()
		if h != nil {
			h(w, r)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(func() {
		g.srv.CloseClientConnections()
		g.srv.Close()
		g.transport.CloseIdleConnections()
	})
	return g
}

// client returns a client for the fake server with short backoffs.
func (g *fakeGateway) client(t *testing.T, o Options) *Client {
	t.Helper()
	o.BaseURL, o.Token = g.srv.URL, testToken
	o.HTTPClient = &http.Client{Transport: g.transport}
	if o.Logger == nil {
		o.Logger = slog.New(slog.DiscardHandler)
	}
	c, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	c.minBackoff, c.maxBackoff, c.rateLimitedBackoff = 5*time.Millisecond, 20*time.Millisecond, 150*time.Millisecond
	return c
}

func (g *fakeGateway) next(t *testing.T) *fakeConn {
	t.Helper()
	select {
	case c := <-g.conns:
		t.Cleanup(func() { _ = c.ws.Close() })
		return c
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for a gateway connection")
		return nil
	}
}

// fakeConn is one connection of the fake gateway; only the test goroutine
// writes to it.
type fakeConn struct {
	ws     *websocket.Conn
	header http.Header
	frames chan frame
}

func (fc *fakeConn) readLoop() {
	defer close(fc.frames)
	for {
		var f frame
		if err := fc.ws.ReadJSON(&f); err != nil {
			return
		}
		fc.frames <- f
	}
}

func (fc *fakeConn) write(t *testing.T, op int, d any) {
	t.Helper()
	raw, _ := json.Marshal(d)
	if err := fc.ws.WriteJSON(frame{Op: op, D: raw}); err != nil {
		t.Logf("fake gateway write: %v", err)
	}
}

func (fc *fakeConn) dispatch(t *testing.T, seq int64, name string, d any) {
	t.Helper()
	raw, _ := json.Marshal(d)
	if err := fc.ws.WriteJSON(frame{Op: opDispatch, D: raw, S: &seq, T: name}); err != nil {
		t.Logf("fake gateway dispatch: %v", err)
	}
}

func (fc *fakeConn) ready(t *testing.T, sessionID string) {
	t.Helper()
	fc.dispatch(t, 1, "READY", map[string]any{"v": 1, "session_id": sessionID, "intents": 512, "heartbeat_interval": 41250,
		"user": map[string]any{"id": "u-bot", "bot_id": "b-1", "username": "bot-helper", "bot": true}})
}

func (fc *fakeConn) closeWith(t *testing.T, code int, text string) {
	t.Helper()
	_ = fc.ws.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(code, text), time.Now().Add(time.Second))
	_ = fc.ws.Close()
}

// expect returns the next frame with op, skipping heartbeats unless op is
// the heartbeat.
func (fc *fakeConn) expect(t *testing.T, op int) frame {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case f, ok := <-fc.frames:
			if !ok {
				t.Fatalf("connection closed while waiting for op %d", op)
			}
			if f.Op == op {
				return f
			}
			if f.Op != opHeartbeat {
				t.Fatalf("got op %d (%s), want %d", f.Op, f.D, op)
			}
		case <-deadline:
			t.Fatalf("timed out waiting for op %d", op)
		}
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// runClient runs c in the background. stop cancels it and returns Run's
// error; done is closed when Run returned (its error is then stop's result).
func runClient(t *testing.T, c *Client) (stop func() error, done <-chan struct{}) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan struct{})
	var result error
	go func() {
		defer close(finished)
		result = c.Run(ctx)
	}()
	stop = func() error {
		cancel()
		select {
		case <-finished:
		case <-time.After(5 * time.Second):
			t.Fatal("Run did not return after cancel")
		}
		return result
	}
	t.Cleanup(func() { _ = stop() })
	return stop, finished
}

func waitFor[T any](t *testing.T, ch <-chan T, what string) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
		var zero T
		return zero
	}
}
