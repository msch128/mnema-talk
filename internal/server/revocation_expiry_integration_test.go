//go:build integration

package server

import (
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/msch128/mnema-talk/internal/auth"
)

// The expiry that takes access away by itself: a session token's end. An open
// socket closes at the token's expiry, armed from the token when the socket
// was opened (internal/ws/lifecycle_pumps.go:106-108, 161-168), with no
// database read, so it also holds while the database is down; the 2-minute
// revalidation compares the same expiry (lifecycle_pumps.go:131-134). The
// member's Talk seat goes with the socket and its media peer at once; the
// listing lingers for the grace period.
func TestASocketClosesWhenItsSessionTokenRunsOut(t *testing.T) {
	a := newApp(t, true)
	a.router.Hub.VoiceGrace = 300 * time.Millisecond
	admin := a.seedAdmin()
	talk := a.createChannel(admin, "Lounge", "voice")
	member := a.register(admin, "short_lived")
	const ttl = 2 * time.Second
	token, err := auth.IssueToken(member.user.ID, 0, "integration-test-secret-integration-test", ttl)
	if err != nil {
		t.Fatal(err)
	}
	issued := time.Now()

	hdr := http.Header{}
	hdr.Set("Origin", a.origin())
	hdr.Add("Cookie", "mnema_session="+token)
	conn, res, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(a.srv.URL, "http")+"/api/ws", hdr)
	if err != nil {
		t.Fatalf("dial with the short token: %v (%v)", err, res)
	}
	t.Cleanup(func() { conn.Close() })
	closed := make(chan time.Time, 1)
	joined := false
	go func() {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				closed <- time.Now()
				return
			}
		}
	}()
	if err := conn.WriteJSON(map[string]any{"type": "voice_join", "payload": map[string]any{"channel_id": talk}}); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline) && !joined; time.Sleep(10 * time.Millisecond) {
		joined = slices.Contains(a.router.Hub.VoiceMembers(talk), member.user.ID)
	}
	if !joined {
		t.Fatal("the short-lived session could not take a seat")
	}

	select {
	case at := <-closed:
		// JWT expiry has second precision: the close falls within a second of ttl.
		if late := at.Sub(issued) - ttl; late > time.Second || late < -time.Second {
			t.Fatalf("the socket closed %v after the token's expiry", late)
		}
		t.Logf("closed %v after issue (token ttl %v)", at.Sub(issued).Round(time.Millisecond), ttl)
	case <-time.After(ttl + 3*time.Second):
		t.Fatal("the socket outlived its session token")
	}
	for deadline := time.Now().Add(time.Second); slices.Contains(a.router.Hub.VoiceMembers(talk), member.user.ID); time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the seat outlived the expired session by more than the grace period")
		}
	}
}
