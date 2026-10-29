//go:build integration

package server

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func registerBody(username, invite string) map[string]string {
	return map[string]string{"username": username, "password": "member-password-123", "invite_code": invite}
}

func TestRegistrationAppliesTheUsernameRules(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	invite := func() string {
		var inv struct{ Code string }
		admin.post("/api/admin/invites", map[string]any{"max_uses": 5}).decode(t, &inv)
		return inv.Code
	}
	code := invite()

	for _, bad := range []string{"a", "dot.", "do..t", "has-hyphen", "all"} {
		if res := a.anon().post("/api/auth/register", registerBody(bad, code)); res.status != http.StatusBadRequest {
			t.Errorf("username %q: status %d, want 400", bad, res.status)
		}
	}

	// Spaces and capitals are normalised, not refused.
	res := a.anon().post("/api/auth/register", registerBody("  Max Muster ", code))
	if res.status != http.StatusCreated {
		t.Fatalf("normalised register: %d %s", res.status, res.body)
	}
	if !strings.Contains(string(res.body), `"username":"max_muster"`) {
		t.Fatalf("username not normalised: %s", res.body)
	}
	if res := a.anon().post("/api/auth/register", registerBody("MAX MUSTER", code)); res.status != http.StatusConflict {
		t.Fatalf("normalised duplicate: %d", res.status)
	}
	if res := a.anon().post("/api/auth/register", registerBody("ab", code)); res.status != http.StatusCreated {
		t.Fatalf("two-character name: %d %s", res.status, res.body)
	}
	a.login("Max_Muster", "member-password-123")
}

func TestLegacyUsernamesKeepWorking(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	legacy := a.register(admin, "oldtimer")
	if _, err := a.db.Exec(context.Background(), `UPDATE users SET username = 'Old-Timer.X' WHERE id = $1`, legacy.user.ID); err != nil {
		t.Fatal(err)
	}

	// Login stays case-insensitive and accepts the old shape.
	for _, name := range []string{"Old-Timer.X", "old-timer.x"} {
		a.login(name, "member-password-123")
	}

	// The name is still mentionable.
	ch := a.createChannel(admin, "general", "text")
	a.send(admin, ch, "hello @old-timer.x, welcome")
	if body := string(legacy.get("/api/mentions").body); !strings.Contains(body, "welcome") {
		t.Fatalf("legacy name not mentioned: %s", body)
	}
}
