package ws

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/httpx"
)

func limitsServer(t *testing.T) (*Hub, string, http.Header) {
	t.Helper()
	h := NewHub(nil, fakeAuth{user: auth.User{ID: uuid.New(), Username: "alice"}}, nil, nil)
	srv := httptest.NewServer(http.HandlerFunc(h.HandleWebSocket))
	t.Cleanup(srv.Close)
	t.Cleanup(h.Close)
	h.Origins = []string{srv.URL}
	return h, "ws" + strings.TrimPrefix(srv.URL, "http"), http.Header{"Origin": []string{srv.URL}}
}

func TestBrowserSocketsPerUserAreCappedAndTheOldestEvicted(t *testing.T) {
	h, url, header := limitsServer(t)
	var conns []*websocket.Conn
	for range maxBrowserSockets + 1 {
		conn, _, err := websocket.DefaultDialer.Dial(url, header)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = conn.Close() })
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		if _, _, err := conn.ReadMessage(); err != nil {
			t.Fatal(err) // registered and writing
		}
		conns = append(conns, conn)
		time.Sleep(2 * time.Millisecond) // distinct connection times
	}
	// The first one was told why and closed; the rest stay.
	var closeErr *websocket.CloseError
	for {
		_, _, err := conns[0].ReadMessage()
		if err == nil {
			continue
		}
		var ok bool
		if closeErr, ok = err.(*websocket.CloseError); !ok {
			t.Fatalf("evicted socket ended with %v", err)
		}
		break
	}
	if closeErr.Code != closeReplaced {
		t.Fatalf("close code = %d, want %d", closeErr.Code, closeReplaced)
	}
	waitUntil(t, func() bool {
		h.mu.RLock()
		defer h.mu.RUnlock()
		return len(h.clients) == maxBrowserSockets
	})
}

func TestWebSocketUpgradesAreRateLimitedPerUser(t *testing.T) {
	h, url, header := limitsServer(t)
	h.UpgradeLimit = httpx.NewRateLimiter(2, time.Minute)
	for range 2 {
		conn, _, err := websocket.DefaultDialer.Dial(url, header)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = conn.Close() })
	}
	conn, resp, err := websocket.DefaultDialer.Dial(url, header)
	if err == nil {
		_ = conn.Close()
		t.Fatal("third upgrade accepted")
	}
	if resp == nil || resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("response = %v, want 429", resp)
	}
	_ = resp.Body.Close()
}

func TestActiveCounterFollowsIdleAcrossConnections(t *testing.T) {
	h := NewHub(nil, nil, nil, nil)
	user := auth.User{ID: uuid.New()}
	a := &Client{hub: h, User: user, send: make(chan []byte, 64)}
	b := &Client{hub: h, User: user, send: make(chan []byte, 64)}
	h.register(a)
	h.register(b)
	h.setIdle(a, true)
	h.setIdle(a, true) // repeated, counted once
	h.mu.RLock()
	status, active := h.statusLocked(user.ID), h.active[user.ID]
	h.mu.RUnlock()
	if status != auth.PresenceOnline || active != 1 {
		t.Fatalf("status %q active %d, want online/1", status, active)
	}
	h.setIdle(b, true)
	h.mu.RLock()
	status = h.statusLocked(user.ID)
	h.mu.RUnlock()
	if status != auth.PresenceAway {
		t.Fatalf("all idle: %q, want away", status)
	}
	h.setIdle(a, false)
	h.unregister(b) // an idle connection leaving changes nothing
	h.mu.RLock()
	status, active = h.statusLocked(user.ID), h.active[user.ID]
	h.mu.RUnlock()
	if status != auth.PresenceOnline || active != 1 {
		t.Fatalf("status %q active %d, want online/1", status, active)
	}
	h.unregister(a)
	h.mu.RLock()
	_, left := h.active[user.ID]
	h.mu.RUnlock()
	if left {
		t.Fatal("active entry of an offline user kept")
	}
}

func waitUntil(t *testing.T, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		if cond() {
			return
		}
	}
	t.Fatal("condition not reached")
}
