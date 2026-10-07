package ws

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/msch128/mnema-talk/internal/auth"
)

// fakeAuth authenticates every request as user with the cookie's version tv.
type fakeAuth struct {
	user auth.User
	tv   int
}

func (f fakeAuth) AuthenticateRequest(*http.Request) (*auth.User, int, error) {
	u := f.user
	return &u, f.tv, nil
}

// Both admission checks and the live connection keep the original verified
// cookie version rather than adopting a newly loaded database version.
func TestConnectionKeepsCookieTokenVersion(t *testing.T) {
	user := auth.User{ID: uuid.New(), Username: "alice"}
	a := &admissionAuth{user: user, tv: 7}
	h := NewHub(nil, a, nil, nil)
	srv := httptest.NewServer(http.HandlerFunc(h.HandleWebSocket))
	defer srv.Close()
	h.Origins = []string{srv.URL}

	hdr := http.Header{"Origin": []string{srv.URL}}
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), hdr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	deadline := time.Now().Add(2 * time.Second)
	for {
		h.mu.RLock()
		var got *Client
		for c := range h.clients {
			got = c
		}
		h.mu.RUnlock()
		if got != nil {
			if got.tokenVersion != 7 {
				t.Fatalf("tokenVersion = %d, want the cookie's 7", got.tokenVersion)
			}
			if n := a.calls.Load(); n != 2 {
				t.Fatalf("authentication checks = %d, want 2", n)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("client never registered")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// admissionAuth pauses a completed check, allowing a revocation to race either
// the initial lookup or the final lookup without arbitrary timing sleeps.
type admissionAuth struct {
	user             auth.User
	tv               int
	pauseAt          int32
	calls            atomic.Int32
	revoked          atomic.Bool
	checked, release chan struct{}
	freshUser        *auth.User
	freshTV          *int
	freshErr         error
}

func (a *admissionAuth) AuthenticateRequest(*http.Request) (*auth.User, int, error) {
	n := a.calls.Add(1)
	u, tv := a.user, a.tv
	var err error
	if a.revoked.Load() {
		err = errors.New("session revoked")
	}
	if n > 1 {
		if a.freshUser != nil {
			u = *a.freshUser
		}
		if a.freshTV != nil {
			tv = *a.freshTV
		}
		if a.freshErr != nil {
			err = a.freshErr
		}
	}
	if n == a.pauseAt {
		close(a.checked)
		<-a.release
	}
	return &u, tv, err
}

func TestWebSocketAdmissionRejectsRevocationDuringEitherCheck(t *testing.T) {
	for _, pauseAt := range []int32{1, 2} {
		t.Run(fmt.Sprintf("check-%d", pauseAt), func(t *testing.T) {
			a := &admissionAuth{user: auth.User{ID: uuid.New(), Username: "alice"}, tv: 7, pauseAt: pauseAt,
				checked: make(chan struct{}), release: make(chan struct{})}
			h := NewHub(nil, a, nil, nil)
			done := make(chan struct{})
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer close(done)
				h.HandleWebSocket(w, r)
			}))
			t.Cleanup(srv.Close)
			t.Cleanup(h.Close)
			var once sync.Once
			release := func() { once.Do(func() { close(a.release) }) }
			t.Cleanup(release)
			h.Origins = []string{srv.URL}
			type dialResult struct {
				conn *websocket.Conn
				err  error
			}
			result := make(chan dialResult, 1)
			go func() {
				conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), http.Header{"Origin": []string{srv.URL}})
				result <- dialResult{conn, err}
			}()
			select {
			case <-a.checked:
			case <-time.After(2 * time.Second):
				t.Fatal("authentication did not reach barrier")
			}
			h.Broadcast("private-before-admission", nil)
			h.SendToUsers([]uuid.UUID{a.user.ID}, "private-targeted", nil)
			h.mu.RLock()
			if len(h.clients) != 0 || len(h.online) != 0 || len(h.profiles) != 0 {
				t.Error("pending handshake published live presence or profile")
			}
			if pauseAt == 2 && len(h.pending) != 1 {
				t.Error("final validation was not tracked for revocation")
			}
			for c := range h.pending {
				if len(c.send) != 0 {
					t.Error("pending handshake queued private events")
				}
			}
			h.mu.RUnlock()
			if len(h.presenceSnapshot()) != 0 {
				t.Error("pending user appeared in presence snapshot")
			}
			a.revoked.Store(true)
			h.DisconnectUser(a.user.ID)
			release()
			var d dialResult
			select {
			case d = <-result:
			case <-time.After(2 * time.Second):
				t.Fatal("dial did not finish")
			}
			if d.err != nil {
				t.Fatalf("unexpected upgrade error: %v", d.err)
			}
			t.Cleanup(func() { _ = d.conn.Close() })
			h.Broadcast("private-after-revocation", nil)
			_ = d.conn.SetReadDeadline(time.Now().Add(2 * time.Second))
			if _, message, err := d.conn.ReadMessage(); err == nil {
				t.Fatalf("revoked handshake received %s", message)
			} else if timeout, ok := err.(interface{ Timeout() bool }); ok && timeout.Timeout() {
				t.Fatal("revoked handshake was not closed")
			}
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("admission did not finish")
			}
			h.mu.RLock()
			defer h.mu.RUnlock()
			if len(h.pending) != 0 || len(h.clients) != 0 || len(h.online) != 0 {
				t.Fatal("revoked handshake retained hub state")
			}
		})
	}
}

func TestWebSocketAdmissionRejectsChangedIdentityVersionAndLookupFailure(t *testing.T) {
	user := auth.User{ID: uuid.New(), Username: "alice"}
	changed := 8
	other := auth.User{ID: uuid.New(), Username: "other"}
	for _, tc := range []struct {
		name string
		user *auth.User
		tv   *int
		err  error
	}{
		{"identity", &other, nil, nil}, {"cookie-version", nil, &changed, nil}, {"lookup-failure", nil, nil, errors.New("database unavailable")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &admissionAuth{user: user, tv: 7, freshUser: tc.user, freshTV: tc.tv, freshErr: tc.err}
			h := NewHub(nil, a, nil, nil)
			srv := httptest.NewServer(http.HandlerFunc(h.HandleWebSocket))
			t.Cleanup(srv.Close)
			t.Cleanup(h.Close)
			h.Origins = []string{srv.URL}
			conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), http.Header{"Origin": []string{srv.URL}})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = conn.Close() })
			_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
			if _, message, err := conn.ReadMessage(); err == nil {
				t.Fatalf("failed authorization received %s", message)
			} else if timeout, ok := err.(interface{ Timeout() bool }); ok && timeout.Timeout() {
				t.Fatal("failed authorization socket was not closed")
			}
			h.mu.RLock()
			defer h.mu.RUnlock()
			if len(h.pending) != 0 || len(h.clients) != 0 || len(h.online) != 0 {
				t.Fatal("failed authorization retained hub state")
			}
		})
	}
}

func TestHubCloseCancelsPendingAdmission(t *testing.T) {
	a := &admissionAuth{user: auth.User{ID: uuid.New(), Username: "alice"}, pauseAt: 2,
		checked: make(chan struct{}), release: make(chan struct{})}
	h := NewHub(nil, a, nil, nil)
	srv := httptest.NewServer(http.HandlerFunc(h.HandleWebSocket))
	t.Cleanup(srv.Close)
	t.Cleanup(h.Close)
	var once sync.Once
	release := func() { once.Do(func() { close(a.release) }) }
	t.Cleanup(release)
	h.Origins = []string{srv.URL}
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), http.Header{"Origin": []string{srv.URL}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	select {
	case <-a.checked:
	case <-time.After(2 * time.Second):
		t.Fatal("final validation did not reach barrier")
	}
	h.Close()
	release()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, message, err := conn.ReadMessage(); err == nil {
		t.Fatalf("shutdown handshake received %s", message)
	} else if timeout, ok := err.(interface{ Timeout() bool }); ok && timeout.Timeout() {
		t.Fatal("shutdown handshake was not closed")
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	if len(h.pending) != 0 || len(h.clients) != 0 {
		t.Fatal("shutdown retained pending admission")
	}
}

func TestWebSocketAdmissionUsesFreshProfileAfterRecheck(t *testing.T) {
	user := auth.User{ID: uuid.New(), Username: "alice", DisplayName: "Old profile"}
	fresh := user
	fresh.DisplayName = "Updated profile"
	a := &admissionAuth{user: user, tv: 7, freshUser: &fresh}
	h := NewHub(nil, a, nil, nil)
	srv := httptest.NewServer(http.HandlerFunc(h.HandleWebSocket))
	t.Cleanup(srv.Close)
	t.Cleanup(h.Close)
	h.Origins = []string{srv.URL}
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), http.Header{"Origin": []string{srv.URL}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := conn.ReadMessage(); err != nil {
		t.Fatal(err)
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	if n := a.calls.Load(); n != 2 {
		t.Fatalf("authentication checks = %d, want 2", n)
	}
	if len(h.pending) != 0 || h.online[user.ID] != 1 {
		t.Fatal("successful admission did not promote exactly once")
	}
	if h.profiles[user.ID].DisplayName != fresh.DisplayName {
		t.Fatal("successful admission published stale profile")
	}
	for c := range h.clients {
		if c.User.DisplayName != fresh.DisplayName || c.tokenVersion != 7 {
			t.Fatal("successful admission changed cookie version or retained stale user")
		}
	}
}
