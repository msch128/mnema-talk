//go:build integration

package server

import (
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/ws"
)

// Pausing after the real check returns models a stale authenticated handshake
// without weakening cookie validation or replacing the database.
type admissionBarrierAuth struct {
	original         ws.Authenticator
	pauseAt          int32
	calls            atomic.Int32
	checked, release chan struct{}
}

func (b *admissionBarrierAuth) AuthenticateRequest(r *http.Request) (*auth.User, int, error) {
	u, tv, err := b.original.AuthenticateRequest(r)
	if b.calls.Add(1) == b.pauseAt {
		close(b.checked)
		<-b.release
	}
	return u, tv, err
}

func TestRevocationDuringWebSocketAdmission(t *testing.T) {
	for _, action := range []string{"disable", "logout-all", "password"} {
		for _, pauseAt := range []int32{1, 2} {
			t.Run(action+map[int32]string{1: "/initial-check", 2: "/final-check"}[pauseAt], func(t *testing.T) {
				a := newApp(t, true)
				admin := a.seedAdmin()
				member := a.register(admin, "admissionmember")
				barrier := &admissionBarrierAuth{
					original: a.router.Hub.Sessions, pauseAt: pauseAt,
					checked: make(chan struct{}), release: make(chan struct{}),
				}
				a.router.Hub.Sessions = barrier
				var releaseOnce sync.Once
				release := func() { releaseOnce.Do(func() { close(barrier.release) }) }
				t.Cleanup(release)
				type dialResult struct {
					conn *wsConn
					err  error
				}
				result := make(chan dialResult, 1)
				go func() { c, _, err := member.dialWS(a.origin()); result <- dialResult{c, err} }()
				select {
				case <-barrier.checked:
				case <-time.After(3 * time.Second):
					t.Fatal("authentication did not reach the admission barrier")
				}
				// Take a separate cookie jar so password rotation does not replace
				// the captured request or the session whose HTTP access is checked.
				session := a.login("admissionmember", "member-password-123")
				var status int
				switch action {
				case "disable":
					status = admin.post("/api/admin/users/"+member.user.ID.String()+"/disable", nil).status
				case "logout-all":
					status = session.post("/api/auth/logout-all", nil).status
				case "password":
					status = session.put("/api/auth/password", map[string]string{"current_password": "member-password-123", "new_password": "changed-member-password-456"}).status
				}
				if status != http.StatusNoContent {
					t.Fatalf("revocation status = %d", status)
				}
				if status := member.get("/api/auth/me").status; status != http.StatusUnauthorized {
					t.Fatalf("old HTTP session status = %d, want 401", status)
				}
				a.router.Hub.Broadcast("admission_private_before_release", nil)
				release()
				var d dialResult
				select {
				case d = <-result:
				case <-time.After(3 * time.Second):
					t.Fatal("dial did not finish")
				}
				if d.err != nil {
					return // Rejection before upgrade is equally safe.
				}
				a.router.Hub.Broadcast("admission_private_after_release", nil)
				// Any frame, including the initial snapshot, exposes a handshake
				// that was admitted after its original cookie was revoked.
				select {
				case ev, ok := <-d.conn.events:
					if ok {
						t.Fatalf("revoked admission received %q", ev.Type)
					}
				case <-time.After(3 * time.Second):
					t.Fatal("revoked admission was not closed")
				}
			})
		}
	}
}
