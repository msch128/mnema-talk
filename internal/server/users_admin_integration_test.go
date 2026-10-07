//go:build integration

package server

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat"
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

func TestDisabledAccountLeavesMembersAndMentionsButPreservesHistory(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	max := a.register(admin, "max")
	moritz := a.register(admin, "moritz")
	channel := a.createChannel(admin, "general", "text")
	messageID := a.send(max, channel, "historical message")
	path := "/api/admin/users/" + max.user.ID.String()

	members := func(c *client) []auth.User {
		t.Helper()
		res := c.get("/api/members")
		if res.status != http.StatusOK {
			t.Fatalf("members: %d %s", res.status, res.body)
		}
		var users []auth.User
		res.decode(t, &users)
		return users
	}
	contains := func(users []auth.User, id uuid.UUID) bool {
		return slices.ContainsFunc(users, func(u auth.User) bool { return u.ID == id })
	}
	if users := members(moritz); len(users) != 3 || users[0].ID != admin.user.ID || !contains(users, max.user.ID) {
		t.Fatalf("initial active roster: %+v", users)
	}
	if res := admin.post(path+"/disable", nil); res.status != http.StatusNoContent {
		t.Fatalf("disable: %d %s", res.status, res.body)
	}
	for _, c := range []*client{admin, moritz} {
		if users := members(c); len(users) != 2 || contains(users, max.user.ID) || !contains(users, moritz.user.ID) {
			t.Fatalf("disabled account in active roster: %+v", users)
		}
	}
	if user := listUsers(t, admin)["max"]; user.ID != max.user.ID || !user.Disabled {
		t.Fatalf("disabled account missing from admin management: %+v", user)
	}
	profile := moritz.get("/api/users/" + max.user.ID.String())
	if profile.status != http.StatusOK {
		t.Fatalf("historical author profile: %d %s", profile.status, profile.body)
	}
	var author auth.User
	profile.decode(t, &author)
	if author.ID != max.user.ID || author.DisplayName != max.user.DisplayName {
		t.Fatalf("historical author changed: %+v", author)
	}
	var messages []struct {
		ID          uuid.UUID `json:"id"`
		UserID      uuid.UUID `json:"user_id"`
		DisplayName string    `json:"display_name"`
		Content     string    `json:"content"`
	}
	res := moritz.get("/api/channels/" + channel.String() + "/messages")
	if res.status != http.StatusOK {
		t.Fatalf("historical messages: %d %s", res.status, res.body)
	}
	res.decode(t, &messages)
	if len(messages) != 1 || messages[0].ID != messageID || messages[0].UserID != max.user.ID ||
		messages[0].DisplayName != max.user.DisplayName || messages[0].Content != "historical message" {
		t.Fatalf("disabled author's history changed: %+v", messages)
	}

	// A just-disabled user's socket may not yet have unregistered. Even a
	// stale presence snapshot must not turn them into a new mention recipient.
	online := []uuid.UUID{admin.user.ID, max.user.ID, moritz.user.ID, moritz.user.ID, uuid.New()}
	for _, content := range []string{"@here", "@all", "@here @MAX"} {
		got, err := chat.ResolveMentions(context.Background(), a.db, content, admin.user.ID, online)
		if err != nil || len(got) != 1 || got[0] != moritz.user.ID {
			t.Fatalf("active recipients for %q: %v, err=%v", content, got, err)
		}
	}
	if got, err := chat.ResolveMentions(context.Background(), a.db, "@max", admin.user.ID, online); err != nil || len(got) != 0 {
		t.Fatalf("disabled named recipient: %v, err=%v", got, err)
	}

	if res := admin.post(path+"/enable", nil); res.status != http.StatusNoContent {
		t.Fatalf("enable: %d %s", res.status, res.body)
	}
	if users := members(moritz); len(users) != 3 || !contains(users, max.user.ID) {
		t.Fatalf("re-enabled member missing: %+v", users)
	}
	if listUsers(t, admin)["max"].Disabled {
		t.Fatal("re-enabled account still marked disabled")
	}
	if res := max.get("/api/auth/me"); res.status != http.StatusUnauthorized {
		t.Fatalf("re-enabling revived a revoked session: %d", res.status)
	}
	if got, err := chat.ResolveMentions(context.Background(), a.db, "@here", admin.user.ID, online); err != nil ||
		len(got) != 2 || !slices.Contains(got, max.user.ID) || !slices.Contains(got, moritz.user.ID) {
		t.Fatalf("re-enabled mention recipient: %v, err=%v", got, err)
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

func TestAdminSetsChosenPassword(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	max := a.register(admin, "max")
	path := "/api/admin/users/" + max.user.ID.String() + "/password"

	if res := admin.post(path, map[string]string{"password": "short"}); res.status != http.StatusBadRequest {
		t.Fatalf("weak password accepted: %d", res.status)
	}
	if res := max.post(path, map[string]string{"password": "chosen-by-admin-123"}); res.status != http.StatusForbidden {
		t.Fatalf("member set a password: %d", res.status)
	}
	if res := admin.post(path, map[string]string{"password": "chosen-by-admin-123"}); res.status != http.StatusNoContent {
		t.Fatalf("set password: %d %s", res.status, res.body)
	}
	if res := max.get("/api/auth/me"); res.status != http.StatusUnauthorized {
		t.Fatalf("session survived password change: %d", res.status)
	}
	if c := a.login("max", "chosen-by-admin-123"); c.get("/api/auth/me").status != http.StatusOK {
		t.Fatal("chosen password does not work")
	}
	self := "/api/admin/users/" + listUsers(t, admin)["Herzog"].ID.String() + "/password"
	if res := admin.post(self, map[string]string{"password": "chosen-by-admin-123"}); res.status != http.StatusBadRequest {
		t.Fatalf("admin changed own password via member route: %d", res.status)
	}
}
