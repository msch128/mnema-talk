//go:build integration

package ws

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/msch128/mnema-talk/internal/testutil"
)

// runWriter starts the client's writer with fast pacing and returns a channel
// that closes when the writer stops.
func runWriter(t *testing.T, h *Hub, c *Client, remote *websocket.Conn) <-chan struct{} {
	t.Helper()
	h.PingInterval, h.RevalidateInterval = time.Hour, 50*time.Millisecond
	done := make(chan struct{})
	go func() { c.writePump(); close(done) }()
	t.Cleanup(func() { c.shutdown(); <-done })
	_ = remote.SetReadDeadline(time.Now().Add(5 * time.Second))
	return done
}

func expectPolicyClose(t *testing.T, remote *websocket.Conn, done <-chan struct{}) {
	t.Helper()
	if _, _, err := remote.ReadMessage(); !websocket.IsCloseError(err, websocket.ClosePolicyViolation) {
		t.Fatalf("close = %v, want policy violation", err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("writer kept running after the session ended")
	}
}

func revokeSession(t *testing.T, h *Hub, jti string) {
	t.Helper()
	if _, err := h.DB.Exec(context.Background(), `INSERT INTO revoked_sessions(jti, expires_at) VALUES($1, NOW() + interval '1 hour')`, jti); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = h.DB.Exec(context.Background(), `DELETE FROM revoked_sessions WHERE jti=$1`, jti) })
}

// A single sign-out records the session id; the next revalidation closes the
// socket although the token version did not change.
func TestRevalidationClosesSignedOutSession(t *testing.T) {
	p := testutil.DB(t)
	u := wsFixtureUser(t, p)
	server, remote := websocketPair(t)
	h := NewHub(p, nil, nil, nil)
	c := newClient(h, server, u, 0)
	c.sessionID = uuid.NewString()
	revokeSession(t, h, c.sessionID)
	expectPolicyClose(t, remote, runWriter(t, h, c, remote))
}

// Another session of the same user being signed out leaves this one alone.
func TestRevalidationKeepsOtherSessions(t *testing.T) {
	p := testutil.DB(t)
	u := wsFixtureUser(t, p)
	server, remote := websocketPair(t)
	h := NewHub(p, nil, nil, nil)
	c := newClient(h, server, u, 0)
	c.sessionID = uuid.NewString()
	revokeSession(t, h, uuid.NewString())
	done := runWriter(t, h, c, remote)
	_ = remote.SetReadDeadline(time.Now().Add(400 * time.Millisecond))
	_, _, err := remote.ReadMessage()
	if websocket.IsCloseError(err, websocket.ClosePolicyViolation) {
		t.Fatal("an unrelated sign-out closed this session")
	}
	select {
	case <-done:
		t.Fatal("writer stopped")
	default:
	}
}

// The token's expiry ends a socket that was opened shortly before it.
func TestWebSocketClosesAtTokenExpiry(t *testing.T) {
	p := testutil.DB(t)
	u := wsFixtureUser(t, p)
	server, remote := websocketPair(t)
	h := NewHub(p, nil, nil, nil)
	c := newClient(h, server, u, 0)
	c.sessionID, c.sessionExp = uuid.NewString(), time.Now().Add(150*time.Millisecond)
	done := runWriter(t, h, c, remote)
	expectPolicyClose(t, remote, done)
}

// A database that stays unreachable ends the socket after a few rounds.
func TestRevalidationFailsClosedOnRepeatedDatabaseErrors(t *testing.T) {
	p := testutil.DB(t)
	u := wsFixtureUser(t, p)
	server, remote := websocketPair(t)
	h := NewHub(closedWSFixturePool(t, p), nil, nil, nil)
	c := newClient(h, server, u, 0)
	expectPolicyClose(t, remote, runWriter(t, h, c, remote))
}

// One failed revalidation is tolerated (a database blip); the counter resets
// on success.
func TestRevalidationToleratesIsolatedDatabaseErrors(t *testing.T) {
	p := testutil.DB(t)
	u := wsFixtureUser(t, p)
	server, _ := websocketPair(t)
	h := NewHub(closedWSFixturePool(t, p), nil, nil, nil)
	c := newClient(h, server, u, 0)
	for i := 1; i < maxRevalidateFailures; i++ {
		if !c.sessionStillValid() {
			t.Fatalf("closed after %d failure(s)", i)
		}
	}
	h.DB = p
	if !c.sessionStillValid() || c.revalidateFails != 0 {
		t.Fatalf("recovery kept the failure count at %d", c.revalidateFails)
	}
}
