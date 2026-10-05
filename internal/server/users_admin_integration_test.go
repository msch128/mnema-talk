//go:build integration

package server

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

type adminUser struct {
	ID         uuid.UUID  `json:"id"`
	Username   string     `json:"username"`
	Role       string     `json:"role"`
	Disabled   bool       `json:"disabled"`
	LastSeenAt *time.Time `json:"last_seen_at"`
}

func listUsers(t *testing.T, admin *client) map[string]adminUser {
	t.Helper()
	res := admin.get("/api/admin/users")
	if res.status != http.StatusOK {
		t.Fatalf("list users: %d %s", res.status, res.body)
	}
	var users []adminUser
	res.decode(t, &users)
	out := map[string]adminUser{}
	for _, u := range users {
		out[u.Username] = u
	}
	return out
}

func TestAdminDisablesAndEnablesUsers(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	max := a.register(admin, "max")
	path := "/api/admin/users/" + max.user.ID.String()

	if res := max.get("/api/admin/users"); res.status != http.StatusForbidden {
		t.Fatalf("non-admin listing users: %d", res.status)
	}
	if res := admin.post(path+"/disable", nil); res.status != http.StatusNoContent {
		t.Fatalf("disable: %d %s", res.status, res.body)
	}
	if res := max.get("/api/auth/me"); res.status != http.StatusUnauthorized {
		t.Fatalf("disabled user's session still valid: %d", res.status)
	}
	login := a.anon().post("/api/auth/login", map[string]string{"username": "max", "password": "member-password-123"})
	if login.status != http.StatusForbidden {
		t.Fatalf("disabled user logged in: %d %s", login.status, login.body)
	}
	if !listUsers(t, admin)["max"].Disabled {
		t.Fatal("list does not show max as disabled")
	}

	if res := admin.post(path+"/enable", nil); res.status != http.StatusNoContent {
		t.Fatalf("enable: %d", res.status)
	}
	if c := a.login("max", "member-password-123"); c.get("/api/auth/me").status != http.StatusOK {
		t.Fatal("re-enabled user cannot log in")
	}

	self := "/api/admin/users/" + listUsers(t, admin)["Herzog"].ID.String()
	if res := admin.post(self+"/disable", nil); res.status != http.StatusBadRequest {
		t.Fatalf("admin disabled themselves: %d", res.status)
	}
}

func TestAdminEndsSessionsAndResetsPasswords(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	max := a.register(admin, "max")
	path := "/api/admin/users/" + max.user.ID.String()

	if res := admin.post(path+"/sessions/revoke", nil); res.status != http.StatusNoContent {
		t.Fatalf("revoke: %d", res.status)
	}
	if res := max.get("/api/auth/me"); res.status != http.StatusUnauthorized {
		t.Fatalf("session survived revoke: %d", res.status)
	}

	res := admin.post(path+"/password-reset", nil)
	if res.status != http.StatusOK {
		t.Fatalf("reset: %d %s", res.status, res.body)
	}
	var out struct{ Password string }
	res.decode(t, &out)
	if len(out.Password) < 16 {
		t.Fatalf("temporary password too short: %q", out.Password)
	}
	if old := a.anon().post("/api/auth/login", map[string]string{"username": "max", "password": "member-password-123"}); old.status != http.StatusUnauthorized {
		t.Fatalf("old password still works: %d", old.status)
	}
	if c := a.login("max", out.Password); c.get("/api/auth/me").status != http.StatusOK {
		t.Fatal("temporary password does not work")
	}
}

func TestAdminKicksFromVoiceAndLastSeen(t *testing.T) {
	a := newApp(t, true)
	admin := a.seedAdmin()
	max := a.register(admin, "max")
	voice := a.createChannel(admin, "Lounge", "voice")

	watcher, _, err := admin.dialWS(a.origin())
	if err != nil {
		t.Fatal(err)
	}
	ws, _, err := max.dialWS(a.origin())
	if err != nil {
		t.Fatal(err)
	}
	ws.send("voice_join", map[string]any{"channel_id": voice})
	if _, ok := watcher.expect("voice_state_update", 2*time.Second); !ok {
		t.Fatal("join not announced")
	}
	if res := admin.post("/api/admin/users/"+max.user.ID.String()+"/kick", nil); res.status != http.StatusNoContent {
		t.Fatalf("kick: %d %s", res.status, res.body)
	}
	payload, ok := watcher.expect("voice_state_update", 2*time.Second)
	if !ok || !strings.Contains(string(payload), `"leave"`) {
		t.Fatalf("kick did not end voice presence: %s", payload)
	}
	if _, ok := ws.expect("voice_kicked", 2*time.Second); !ok {
		t.Fatal("kicked client was not told")
	}

	ws.conn.Close()
	time.Sleep(300 * time.Millisecond)
	if listUsers(t, admin)["max"].LastSeenAt == nil {
		t.Fatal("last_seen_at not recorded on disconnect")
	}
}
