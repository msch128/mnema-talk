//go:build integration

package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/msch128/mnema-talk/internal/auth"
	"golang.org/x/crypto/bcrypt"
)

// expectClosed waits until the server closes the socket.
func (w *wsConn) expectClosed(within time.Duration) bool {
	timeout := time.After(within)
	for {
		select {
		case _, ok := <-w.events:
			if !ok {
				return true
			}
		case <-timeout:
			return false
		}
	}
}

func TestLogoutAllClosesOpenWebSockets(t *testing.T) {
	a := newApp(t, true)
	admin := a.seedAdmin()
	ws, _, err := admin.dialWS(a.origin())
	if err != nil {
		t.Fatal(err)
	}
	if res := admin.post("/api/auth/logout-all", nil); res.status != http.StatusNoContent {
		t.Fatalf("logout-all: %d %s", res.status, res.body)
	}
	if !ws.expectClosed(5 * time.Second) {
		t.Fatal("websocket stayed open after logout-all")
	}
}

func TestChangePasswordClosesWebSocketsAndKeepsSession(t *testing.T) {
	a := newApp(t, true)
	admin := a.seedAdmin()
	other := a.login("Herzog", "admin-password-123")
	ws, _, err := other.dialWS(a.origin())
	if err != nil {
		t.Fatal(err)
	}
	own, _, err := admin.dialWS(a.origin())
	if err != nil {
		t.Fatal(err)
	}
	res := admin.put("/api/auth/password", map[string]string{"current_password": "admin-password-123", "new_password": "new-admin-password-456"})
	if res.status != http.StatusNoContent {
		t.Fatalf("change password: %d %s", res.status, res.body)
	}
	if !ws.expectClosed(5*time.Second) || !own.expectClosed(5*time.Second) {
		t.Fatal("websockets stayed open after a password change")
	}
	if _, _, err := admin.dialWS(a.origin()); err != nil {
		t.Fatalf("reconnect with the fresh cookie: %v", err)
	}
	if _, res, err := other.dialWS(a.origin()); err == nil || res == nil || res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("revoked session reconnected: err=%v", err)
	}
}

func TestAuthenticateRequestReturnsTokenVersion(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	sessions := &auth.Sessions{DB: a.db, Secret: "integration-test-secret-integration-test"}
	req := func() *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		for _, ck := range admin.http.Jar.Cookies(mustURL(a.srv.URL)) {
			r.AddCookie(ck)
		}
		return r
	}
	u, tv, err := sessions.AuthenticateRequest(req())
	if err != nil || u.Username != "Herzog" || tv != 0 {
		t.Fatalf("got %v, %d, %v", u, tv, err)
	}
	if res := admin.put("/api/auth/password", map[string]string{"current_password": "admin-password-123", "new_password": "new-admin-password-456"}); res.status != http.StatusNoContent {
		t.Fatalf("change password: %d", res.status)
	}
	if _, tv, err := sessions.AuthenticateRequest(req()); err != nil || tv != 1 {
		t.Fatalf("after password change: tv=%d err=%v", tv, err)
	}
	if u, err := sessions.Authenticate(req()); err != nil || u.Username != "Herzog" {
		t.Fatalf("Authenticate: %v %v", u, err)
	}
}

func TestEnsureAdminUserValidatesAndUsesAuthHashing(t *testing.T) {
	a := newApp(t, false)
	ctx := context.Background()

	for _, tc := range []struct{ user, pass, want string }{
		{"x", "", "ADMIN_USERNAME"},
		{"all", "", "ADMIN_USERNAME"},
		{strings.Repeat("a", 40), "", "ADMIN_USERNAME"},
		{"Herzog", "short", "ADMIN_INITIAL_PASSWORD"},
		{"Herzog", strings.Repeat("p", 73), "ADMIN_INITIAL_PASSWORD"},
	} {
		if err := auth.EnsureAdminUser(ctx, a.db, tc.user, tc.pass, false); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("EnsureAdminUser(%q): %v, want an error naming %s", tc.user, err, tc.want)
		}
	}

	// Production never generates (and so never logs) a password.
	if err := auth.EnsureAdminUser(ctx, a.db, "Herzog", "", true); err == nil || !strings.Contains(err.Error(), "ADMIN_INITIAL_PASSWORD") {
		t.Errorf("empty password in production: %v, want an error naming ADMIN_INITIAL_PASSWORD", err)
	}

	if err := auth.EnsureAdminUser(ctx, a.db, "Herzog", "admin-password-123", false); err != nil {
		t.Fatal(err)
	}
	var hash, display string
	if err := a.db.QueryRow(ctx, `SELECT password_hash, display_name FROM users WHERE username = 'Herzog'`).Scan(&hash, &display); err != nil {
		t.Fatal(err)
	}
	if cost, _ := bcrypt.Cost([]byte(hash)); cost != auth.PasswordCost() {
		t.Errorf("admin hash cost %d, want auth's %d", cost, auth.PasswordCost())
	}
	if display != "Herzog" {
		t.Errorf("display name %q", display)
	}
	// Idempotent, and settings no longer matter once the admin exists.
	if err := auth.EnsureAdminUser(ctx, a.db, "x", "short", false); err != nil {
		t.Fatalf("existing admin: %v", err)
	}
	if err := auth.EnsureAdminUser(ctx, a.db, "Herzog", "", true); err != nil {
		t.Fatalf("existing admin, production, empty password: %v", err)
	}
}

func TestEnsureAdminUserRefusesCaseInsensitiveClash(t *testing.T) {
	a := newApp(t, false)
	ctx := context.Background()
	hash, _ := auth.HashPassword("member-password-1")
	if _, err := a.db.Exec(ctx, `INSERT INTO users (username, display_name, password_hash, role) VALUES ('herzog', 'herzog', $1, 'user')`, hash); err != nil {
		t.Fatal(err)
	}
	err := auth.EnsureAdminUser(ctx, a.db, "Herzog", "admin-password-123", false)
	if err == nil || !strings.Contains(err.Error(), "taken") {
		t.Fatalf("got %v, want a 'taken' error", err)
	}
	var admins int
	_ = a.db.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE role = 'admin'`).Scan(&admins)
	if admins != 0 {
		t.Fatal("an admin was created next to the clashing user")
	}
}
