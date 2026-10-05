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

// The connection keeps the token version of the verified cookie, so a
// revocation racing the upgrade is caught by the next revalidation.
func TestConnectionKeepsCookieTokenVersion(t *testing.T) {
	user := auth.User{ID: uuid.New(), Username: "alice"}
	h := NewHub(nil, fakeAuth{user: user, tv: 7}, nil, nil)
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
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("client never registered")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
