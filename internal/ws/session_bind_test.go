package ws

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
)

type claimingAuth struct {
	fakeAuth
	id  string
	exp time.Time
}

func (a claimingAuth) SessionClaims(*http.Request) (string, time.Time) { return a.id, a.exp }

func TestBindSessionTakesClaimsFromTheAuthenticator(t *testing.T) {
	exp := time.Now().Add(time.Hour)
	h := NewHub(nil, claimingAuth{id: "jti-1", exp: exp}, nil, nil)
	c := newClient(h, nil, auth.User{}, 0)
	h.bindSession(c, httptest.NewRequest(http.MethodGet, "/api/ws", nil))
	if c.sessionID != "jti-1" || !c.sessionExp.Equal(exp) {
		t.Fatalf("bound %q %v", c.sessionID, c.sessionExp)
	}
	plain := NewHub(nil, fakeAuth{}, nil, nil)
	d := newClient(plain, nil, auth.User{}, 0)
	plain.bindSession(d, httptest.NewRequest(http.MethodGet, "/api/ws", nil))
	if d.sessionID != "" || !d.sessionExp.IsZero() {
		t.Fatal("an authenticator without claims must bind nothing")
	}
}

func TestSessionExpiredOnlyWithAnExpiryInThePast(t *testing.T) {
	c := newClient(nil, nil, auth.User{}, 0)
	now := time.Now()
	if c.sessionExpired(now) {
		t.Fatal("no expiry must never expire")
	}
	c.sessionExp = now.Add(time.Minute)
	if c.sessionExpired(now) {
		t.Fatal("future expiry reported as expired")
	}
	if !c.sessionExpired(now.Add(time.Minute)) {
		t.Fatal("reached expiry not reported")
	}
}

func TestDisconnectSessionClosesOnlyMatchingConnections(t *testing.T) {
	h := NewHub(nil, nil, nil, nil)
	u := auth.User{ID: uuid.New()}
	mine, other := testClient(h, u), testClient(h, u)
	mine.sessionID, other.sessionID = "session-a", "session-b"
	pending := newClient(h, nil, u, 0)
	pending.sessionID = "session-a"
	h.pending[pending] = struct{}{}
	h.DisconnectSession("")
	if mine.closed.Load() {
		t.Fatal("an empty session id must match nothing")
	}
	h.DisconnectSession("session-a")
	if !mine.closed.Load() || !pending.closed.Load() || other.closed.Load() {
		t.Fatalf("closed: matching=%v pending=%v other=%v", mine.closed.Load(), pending.closed.Load(), other.closed.Load())
	}
	if _, still := h.pending[pending]; still {
		t.Fatal("pending handshake of the signed-out session kept")
	}
}
