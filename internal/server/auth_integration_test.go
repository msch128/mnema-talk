//go:build integration

package server

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/msch128/mnema-talk/internal/httpx"
)

func TestMigrationsAreIdempotent(t *testing.T) {
	a := newApp(t, false)
	if err := a.db.Migrate(context.Background()); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	var n int
	if err := a.db.QueryRow(context.Background(), `SELECT COUNT(*) FROM schema_migrations`).Scan(&n); err != nil || n < 2 {
		t.Fatalf("schema_migrations rows=%d err=%v", n, err)
	}
}

func TestSessionCookieIsHttpOnlyAndBodyHasNoToken(t *testing.T) {
	a := newApp(t, false)
	a.seedAdmin()

	c := a.anon()
	res := c.post("/api/auth/login", map[string]string{"username": "herzog", "password": "admin-password-123"})
	if res.status != http.StatusOK {
		t.Fatalf("login: %d %s", res.status, res.body)
	}
	if strings.Contains(string(res.body), "token") {
		t.Fatal("the session token must never be exposed to JavaScript")
	}
	cookie := res.header.Get("Set-Cookie")
	for _, attr := range []string{"HttpOnly", "SameSite=Lax", "Path=/"} {
		if !strings.Contains(cookie, attr) {
			t.Errorf("cookie lacks %s: %s", attr, cookie)
		}
	}

	if me := c.get("/api/auth/me"); me.status != http.StatusOK {
		t.Fatalf("me: %d", me.status)
	}
	if res := c.post("/api/auth/logout", nil); res.status != http.StatusNoContent {
		t.Fatalf("logout: %d", res.status)
	}
	if me := c.get("/api/auth/me"); me.status != http.StatusUnauthorized {
		t.Fatalf("me after logout: %d", me.status)
	}
}

func TestLoginFailuresDoNotRevealAccounts(t *testing.T) {
	a := newApp(t, false)
	a.seedAdmin()
	c := a.anon()
	wrongPass := c.post("/api/auth/login", map[string]string{"username": "Herzog", "password": "nope-nope-nope"})
	noUser := c.post("/api/auth/login", map[string]string{"username": "ghost", "password": "nope-nope-nope"})
	if wrongPass.status != http.StatusUnauthorized || noUser.status != http.StatusUnauthorized {
		t.Fatalf("statuses %d / %d", wrongPass.status, noUser.status)
	}
	if string(wrongPass.body) != string(noUser.body) {
		t.Fatalf("responses differ: %s vs %s", wrongPass.body, noUser.body)
	}
}

func TestLoginLockoutAfterRepeatedFailures(t *testing.T) {
	a := newApp(t, false)
	a.seedAdmin()
	c := a.anon()
	for i := 0; i < 10; i++ {
		c.post("/api/auth/login", map[string]string{"username": "Herzog", "password": "wrong-password"})
	}
	// Even the correct password is refused while locked.
	res := c.post("/api/auth/login", map[string]string{"username": "Herzog", "password": "admin-password-123"})
	if res.status != http.StatusTooManyRequests || res.header.Get("Retry-After") == "" {
		t.Fatalf("expected 429 with Retry-After, got %d", res.status)
	}
}

// TestFailedLoginsFromOneClientDoNotLockOutOthers: lockouts are per client IP
// and username, so guessing the admin's password from one address does not
// lock the admin out everywhere.
func TestFailedLoginsFromOneClientDoNotLockOutOthers(t *testing.T) {
	a := newApp(t, false)
	a.seedAdmin()
	attacker := a.anon()
	from := func(ip string) map[string]string { return map[string]string{"X-Forwarded-For": ip} }
	body := func(pw string) any { return map[string]string{"username": "Herzog", "password": pw} }
	for i := 0; i < 10; i++ {
		attacker.do(http.MethodPost, "/api/auth/login", body("wrong-password"), from("203.0.113.5"))
	}
	if res := attacker.do(http.MethodPost, "/api/auth/login", body("admin-password-123"), from("203.0.113.5")); res.status != http.StatusTooManyRequests {
		t.Fatalf("attacker address not locked: %d", res.status)
	}
	if res := a.anon().do(http.MethodPost, "/api/auth/login", body("admin-password-123"), from("198.51.100.7")); res.status != http.StatusOK {
		t.Fatalf("admin locked out from another address: %d %s", res.status, res.body)
	}
}

func TestCSRFRejectsCrossSiteWrites(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	ch := a.createChannel(admin, "general", "text")

	evil := admin.do(http.MethodPost, "/api/channels/"+ch.String()+"/messages",
		map[string]string{"content": "forged"}, map[string]string{"Origin": "https://evil.example"})
	if evil.status != http.StatusForbidden {
		t.Fatalf("cross-site POST: %d", evil.status)
	}
	noOrigin := admin.do(http.MethodPost, "/api/channels/"+ch.String()+"/messages",
		map[string]string{"content": "forged"}, map[string]string{"Origin": ""})
	if noOrigin.status != http.StatusForbidden {
		t.Fatalf("POST without Origin/Referer: %d", noOrigin.status)
	}
}

func TestRegistrationRequiresValidInvite(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()

	c := a.anon()
	res := c.post("/api/auth/register", map[string]string{"username": "max", "password": "member-password-123", "invite_code": "made-up-code"})
	if res.status != http.StatusBadRequest {
		t.Fatalf("bad invite: %d %s", res.status, res.body)
	}

	inv := admin.post("/api/admin/invites", map[string]any{"max_uses": 1})
	var invite struct{ Code string }
	inv.decode(t, &invite)

	ok := a.anon().post("/api/auth/register", map[string]string{"username": "max", "password": "member-password-123", "invite_code": invite.Code})
	if ok.status != http.StatusCreated {
		t.Fatalf("register: %d %s", ok.status, ok.body)
	}
	again := a.anon().post("/api/auth/register", map[string]string{"username": "moritz", "password": "member-password-123", "invite_code": invite.Code})
	if again.status != http.StatusBadRequest {
		t.Fatalf("exhausted invite reused: %d", again.status)
	}
	dup := admin.post("/api/admin/invites", map[string]any{})
	var inv2 struct{ Code string }
	dup.decode(t, &inv2)
	taken := a.anon().post("/api/auth/register", map[string]string{"username": "MAX", "password": "member-password-123", "invite_code": inv2.Code})
	if taken.status != http.StatusConflict {
		t.Fatalf("case-insensitive duplicate username: %d", taken.status)
	}
	weak := a.anon().post("/api/auth/register", map[string]string{"username": "weakling", "password": "123", "invite_code": inv2.Code})
	if weak.status != http.StatusBadRequest {
		t.Fatalf("weak password: %d", weak.status)
	}
}

func TestLogoutEverywhereRevokesAllSessions(t *testing.T) {
	a := newApp(t, false)
	a.seedAdmin()
	laptop := a.login("Herzog", "admin-password-123")
	phone := a.login("Herzog", "admin-password-123")

	if res := laptop.post("/api/auth/logout-all", nil); res.status != http.StatusNoContent {
		t.Fatalf("logout-all: %d %s", res.status, res.body)
	}
	for name, c := range map[string]*client{"laptop": laptop, "phone": phone} {
		if res := c.get("/api/auth/me"); res.status != http.StatusUnauthorized {
			t.Fatalf("%s session still valid after logout-all: %d", name, res.status)
		}
	}
	if res := a.anon().post("/api/auth/logout-all", nil); res.status != http.StatusUnauthorized {
		t.Fatalf("logout-all without a session: %d", res.status)
	}
}

func TestPasswordChangeRevokesOtherSessions(t *testing.T) {
	a := newApp(t, false)
	a.seedAdmin()
	laptop := a.login("Herzog", "admin-password-123")
	phone := a.login("Herzog", "admin-password-123")

	res := laptop.put("/api/auth/password", map[string]string{"current_password": "admin-password-123", "new_password": "a-brand-new-password"})
	if res.status != http.StatusNoContent {
		t.Fatalf("change password: %d %s", res.status, res.body)
	}
	if me := laptop.get("/api/auth/me"); me.status != http.StatusOK {
		t.Fatalf("the session that changed the password must stay valid: %d", me.status)
	}
	if me := phone.get("/api/auth/me"); me.status != http.StatusUnauthorized {
		t.Fatalf("other sessions must be revoked: %d", me.status)
	}

	wrong := laptop.put("/api/auth/password", map[string]string{"current_password": "not-it-at-all", "new_password": "another-new-password"})
	if wrong.status != http.StatusForbidden {
		t.Fatalf("wrong current password: %d", wrong.status)
	}
}

func TestAdminRoutesRequireAdmin(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	member := a.register(admin, "max")

	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/admin/invites"},
		{http.MethodPost, "/api/admin/channels"},
		{http.MethodGet, "/api/admin/media"},
		{http.MethodPost, "/api/admin/media/prune?days=1"},
	} {
		res := member.do(tc.method, tc.path, map[string]any{}, nil)
		if res.status != http.StatusForbidden {
			t.Errorf("%s %s as member: %d", tc.method, tc.path, res.status)
		}
	}
	if res := a.anon().get("/api/admin/invites"); res.status != http.StatusUnauthorized {
		t.Errorf("anonymous admin access: %d", res.status)
	}
}

func TestUnknownJSONFieldsAreRejected(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	member := a.register(admin, "max")
	// A member cannot smuggle a role through the profile endpoint.
	res := member.put("/api/users/me/profile", map[string]string{"display_name": "Max", "role": "admin"})
	if res.status != http.StatusBadRequest || res.code(t) != httpx.CodeInvalidInput {
		t.Fatalf("got %d %s", res.status, res.body)
	}
}

func TestUserLocale(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	var me struct{ Locale string }
	admin.get("/api/auth/me").decode(t, &me)
	if me.Locale != "" {
		t.Fatalf("new users start without a chosen locale, got %q", me.Locale)
	}
	if res := admin.put("/api/users/me/locale", map[string]string{"locale": "en"}); res.status != http.StatusOK {
		t.Fatalf("set locale: %d %s", res.status, res.body)
	}
	admin.get("/api/auth/me").decode(t, &me)
	if me.Locale != "en" {
		t.Fatalf("locale %q, want en", me.Locale)
	}
	if res := admin.put("/api/users/me/locale", map[string]string{"locale": "fr"}); res.status != http.StatusBadRequest {
		t.Fatalf("unsupported locale accepted: %d", res.status)
	}
}
