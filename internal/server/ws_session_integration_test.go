//go:build integration

package server

import (
	"net/http"
	"testing"
	"time"
)

// Signing out one session closes the sockets opened with it at once, while the
// same user's other sessions keep theirs.
func TestLogoutClosesOnlyThatSessionsWebSockets(t *testing.T) {
	a := newApp(t, true)
	admin := a.seedAdmin()
	a.register(admin, "logoutmember")
	first := a.login("logoutmember", "member-password-123")
	second := a.login("logoutmember", "member-password-123")
	closing := first.dialLiveWS(a.origin())
	staying := second.dialLiveWS(a.origin())

	if st := first.post("/api/auth/logout", nil).status; st != http.StatusNoContent {
		t.Fatalf("logout status = %d", st)
	}
	deadline := time.After(3 * time.Second)
	for open := true; open; {
		select {
		case _, open = <-closing.events:
		case <-deadline:
			t.Fatal("the signed-out session's websocket stayed open")
		}
	}
	staying.send("ping", map[string]int{"t": 1})
	if _, ok := staying.expect("pong", soon); !ok {
		t.Fatal("logout closed another session's websocket")
	}
}
