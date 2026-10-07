package ws

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/msch128/mnema-talk/internal/auth"
)

// Events reach a client from goroutines other than its read loop (SFU
// signalling callbacks, admin kicks). Unregistering must not make those
// sends panic with "send on closed channel".
func TestSendAfterUnregisterDoesNotPanic(t *testing.T) {
	h := NewHub(nil, nil, nil, nil)
	for i := 0; i < 50; i++ {
		c := newClient(h, nil, auth.User{ID: uuid.New(), Username: "u"}, 0)
		h.register(c)
		var wg sync.WaitGroup
		for j := 0; j < 4; j++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for k := 0; k < 100; k++ {
					c.SendEvent("webrtc_candidate", map[string]any{"k": k})
				}
			}()
		}
		h.unregister(c)
		wg.Wait()
		c.SendEvent("voice_kicked", nil) // after unregister: dropped, no panic
		select {
		case <-c.done:
		default:
			t.Fatal("done not closed after unregister")
		}
	}
}

func TestHubCloseTerminatesWebSocketAndRejectsNewConnections(t *testing.T) {
	h := NewHub(nil, fakeAuth{user: auth.User{ID: uuid.New(), Username: "alice"}}, nil, nil)
	srv := httptest.NewServer(http.HandlerFunc(h.HandleWebSocket))
	t.Cleanup(srv.Close)
	t.Cleanup(h.Close)
	h.Origins = []string{srv.URL}
	url := "ws" + strings.TrimPrefix(srv.URL, "http")
	header := http.Header{"Origin": []string{srv.URL}}
	conn, _, err := websocket.DefaultDialer.Dial(url, header)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := conn.ReadMessage(); err != nil {
		t.Fatal(err) // Initial event proves registration and writer startup.
	}
	h.mu.RLock()
	var client *Client
	for c := range h.clients {
		client = c
	}
	h.mu.RUnlock()
	if client == nil {
		t.Fatal("client not registered")
	}
	h.Close()
	h.Close()
	select {
	case <-client.done:
	default:
		t.Fatal("shutdown did not stop the client writer")
	}
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			if timeout, ok := err.(interface{ Timeout() bool }); ok && timeout.Timeout() {
				t.Fatal("WebSocket remained open until read deadline")
			}
			break
		}
	}
	if other, response, err := websocket.DefaultDialer.Dial(url, header); err == nil {
		_ = other.Close()
		t.Fatal("shutdown accepted a new WebSocket")
	} else if response == nil || response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("post-shutdown handshake status = %v, want 503", response)
	} else {
		_ = response.Body.Close()
	}
}

// Shutdown must not wait on voiceMu/transport cleanup, and a read loop
// finishing later must never start another grace timer.
func TestHubCloseDoesNotWaitOnVoiceCleanupOrRestartGrace(t *testing.T) {
	h := NewHub(nil, nil, nil, nil)
	c := newClient(h, nil, auth.User{ID: uuid.New(), Username: "alice"}, 0)
	h.register(c)
	key := voiceKey{c.User.ID, uuid.New()}
	h.startGrace(key)
	c.voiceMu.Lock()
	defer c.voiceMu.Unlock()
	done := make(chan struct{})
	go func() { h.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("hub shutdown waited on voice cleanup")
	}
	h.startGrace(key)
	h.mu.RLock()
	defer h.mu.RUnlock()
	if len(h.grace) != 0 {
		t.Fatal("shutdown retained or restarted a grace timer")
	}
}

func TestHubCloseRacesRegistration(t *testing.T) {
	for range 50 {
		h := NewHub(nil, nil, nil, nil)
		c := newClient(h, nil, auth.User{ID: uuid.New(), Username: "alice"}, 0)
		var group sync.WaitGroup
		group.Add(2)
		go func() {
			defer group.Done()
			if !h.register(c) {
				c.shutdown()
			}
		}()
		go func() { defer group.Done(); h.Close() }()
		group.Wait()
		if !c.closed.Load() {
			t.Fatal("racing registration left a client running")
		}
		if h.register(newClient(h, nil, auth.User{ID: uuid.New()}, 0)) {
			t.Fatal("registered after shutdown")
		}
	}
}

type shutdownAuth struct {
	entered chan struct{}
	release chan struct{}
}

func (a shutdownAuth) AuthenticateRequest(*http.Request) (*auth.User, int, error) {
	close(a.entered)
	<-a.release
	return &auth.User{ID: uuid.New(), Username: "alice"}, 0, nil
}

func TestHubCloseDuringAuthenticationRejectsUpgrade(t *testing.T) {
	a := shutdownAuth{entered: make(chan struct{}), release: make(chan struct{})}
	h := NewHub(nil, a, nil, nil)
	srv := httptest.NewServer(http.HandlerFunc(h.HandleWebSocket))
	t.Cleanup(srv.Close)
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(a.release) }) }
	t.Cleanup(release)
	t.Cleanup(h.Close)
	h.Origins = []string{srv.URL}
	status := make(chan int, 1)
	go func() {
		conn, response, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), http.Header{"Origin": []string{srv.URL}})
		if conn != nil {
			_ = conn.Close()
		}
		if response != nil {
			defer response.Body.Close()
			status <- response.StatusCode
		} else if err != nil {
			status <- 0
		}
	}()
	select {
	case <-a.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("authentication did not start")
	}
	h.Close()
	release()
	select {
	case got := <-status:
		if got != http.StatusServiceUnavailable {
			t.Fatalf("racing upgrade status = %d, want 503", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("racing upgrade did not finish")
	}
	if h.OnlineCount() != 0 {
		t.Fatal("racing upgrade registered a client")
	}
}
