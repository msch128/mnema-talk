//go:build contract

package mnema

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// contractEnv is the environment of the contract tests (see README,
// Contract tests).
type contractEnv struct {
	url, token, channel, cookie string
}

func loadContractEnv(t *testing.T) contractEnv {
	t.Helper()
	env := contractEnv{
		url:     strings.TrimRight(os.Getenv("MNEMA_CONTRACT_URL"), "/"),
		token:   os.Getenv("MNEMA_CONTRACT_TOKEN"),
		channel: os.Getenv("MNEMA_CONTRACT_CHANNEL"),
		cookie:  os.Getenv("MNEMA_CONTRACT_MEMBER_COOKIE"),
	}
	if env.url == "" {
		t.Skip("MNEMA_CONTRACT_URL is not set: the contract tests need a running server")
	}
	if env.token == "" || env.channel == "" || env.cookie == "" {
		t.Fatal("MNEMA_CONTRACT_URL is set, but MNEMA_CONTRACT_TOKEN, MNEMA_CONTRACT_CHANNEL or MNEMA_CONTRACT_MEMBER_COOKIE is not")
	}
	return env
}

// member is the signed-in member of the contract tests: the member routes
// (with the Origin the CSRF check wants) and the member WebSocket.
type member struct {
	env contractEnv
	ws  *websocket.Conn

	mu     sync.Mutex
	events []memberEvent
}

type memberEvent struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// call sends a member request and fails unless the status is expect; the
// answer is decoded into out when out is not nil.
func (m *member) call(t *testing.T, method, path string, body any, expect int, out any) {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, m.env.url+path, r)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Cookie", m.env.cookie)
	req.Header.Set("Origin", m.env.url)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("member %s %s: %v", method, path, err)
	}
	defer res.Body.Close()
	text, _ := io.ReadAll(res.Body)
	if res.StatusCode != expect {
		t.Fatalf("member %s %s: expected %d, got %d: %s", method, path, expect, res.StatusCode, text)
	}
	if out != nil {
		if err := json.Unmarshal(text, out); err != nil {
			t.Fatalf("member %s %s: undecodable answer %s: %v", method, path, text, err)
		}
	}
}

// message is the member's view of one message, through the list around it
// (there is no member route for a single message).
func (m *member) message(t *testing.T, id string) map[string]any {
	t.Helper()
	var page []map[string]any
	m.call(t, http.MethodGet, fmt.Sprintf("/api/channels/%s/messages?around=%s&limit=5", m.env.channel, id), nil, http.StatusOK, &page)
	for _, x := range page {
		if x["id"] == id {
			return x
		}
	}
	t.Fatalf("the member cannot see message %s", id)
	return nil
}

// listen opens the member WebSocket and keeps its events.
func (m *member) listen(t *testing.T) {
	t.Helper()
	u := "ws" + strings.TrimPrefix(m.env.url, "http") + "/api/ws"
	ws, res, err := websocket.DefaultDialer.Dial(u, http.Header{"Cookie": {m.env.cookie}, "Origin": {m.env.url}})
	if res != nil && res.Body != nil {
		_ = res.Body.Close()
	}
	if err != nil {
		t.Fatalf("member WebSocket: %v", err)
	}
	m.ws = ws
	go func() {
		for {
			_, data, err := ws.ReadMessage()
			if err != nil {
				return
			}
			for _, line := range strings.Split(string(data), "\n") {
				var e memberEvent
				if strings.TrimSpace(line) == "" || json.Unmarshal([]byte(line), &e) != nil {
					continue
				}
				m.mu.Lock()
				m.events = append(m.events, e)
				m.mu.Unlock()
			}
		}
	}()
}

// event waits for an event of the type whose payload matches.
func (m *member) event(t *testing.T, typ string, match func(json.RawMessage) bool) json.RawMessage {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		m.mu.Lock()
		for _, e := range m.events {
			if e.Type == typ && match(e.Payload) {
				m.mu.Unlock()
				return e.Payload
			}
		}
		m.mu.Unlock()
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for the member event %s", typ)
	return nil
}

func (m *member) close() {
	if m.ws != nil {
		_ = m.ws.Close()
	}
}

// statusCounter counts the answers of each status the transport saw.
type statusCounter struct {
	mu     sync.Mutex
	counts map[int]int
}

func (s *statusCounter) RoundTrip(r *http.Request) (*http.Response, error) {
	res, err := http.DefaultTransport.RoundTrip(r)
	if err == nil {
		s.mu.Lock()
		s.counts[res.StatusCode]++
		s.mu.Unlock()
	}
	return res, err
}

func (s *statusCounter) count(status int) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.counts[status]
}

// await returns the first value from ch that matches, or fails after 10 s.
func await[T any](t *testing.T, ch <-chan T, what string, match func(T) bool) T {
	t.Helper()
	timeout := time.After(10 * time.Second)
	for {
		select {
		case v := <-ch:
			if match(v) {
				return v
			}
		case <-timeout:
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}

func background(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return ctx
}
