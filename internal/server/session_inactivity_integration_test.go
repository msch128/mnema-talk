//go:build integration

package server

import (
	"net/http"
	"testing"

	"github.com/msch128/mnema-talk/internal/auth"
)

func putInactivity(c *client, days any) response {
	return c.put("/api/admin/onboarding", map[string]any{"community_name": "Home", "session_inactivity_days": days})
}

func inactivityDays(t *testing.T, c *client) int {
	t.Helper()
	res := c.get("/api/admin/onboarding")
	wantStatus(t, "get settings", res, http.StatusOK)
	var s struct {
		Days int `json:"session_inactivity_days"`
	}
	res.decode(t, &s)
	return s.Days
}

func TestInactivityLimitDefaultsToSevenDaysAndStaysWithinBounds(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	if got := inactivityDays(t, admin); got != 7 {
		t.Fatalf("default %d, want 7", got)
	}
	for _, bad := range []any{0, -1, 366, 100000} {
		if res := putInactivity(admin, bad); res.status != http.StatusBadRequest {
			t.Errorf("%v days: %d, want 400", bad, res.status)
		}
	}
	for _, good := range []int{1, 365, 30} {
		wantStatus(t, "set", putInactivity(admin, good), http.StatusOK)
		if got := inactivityDays(t, admin); got != good {
			t.Errorf("stored %d, want %d", got, good)
		}
	}
	// Saving the other settings without the field keeps the limit.
	wantStatus(t, "save without", admin.put("/api/admin/onboarding", map[string]any{"community_name": "Renamed"}), http.StatusOK)
	if got := inactivityDays(t, admin); got != 30 {
		t.Errorf("limit changed to %d by an unrelated save", got)
	}
	member := a.register(admin, "alice")
	wantStatus(t, "member", putInactivity(member, 3), http.StatusForbidden)
}

func TestAnInactiveMemberIsSignedOutAndSeesTheirOtherDevices(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	phone := a.login("cool_guy", "admin-password-123")
	wantStatus(t, "me", phone.get("/api/auth/me"), http.StatusOK)

	res := admin.get("/api/auth/sessions")
	wantStatus(t, "sessions", res, http.StatusOK)
	var list auth.MemberSessionList
	res.decode(t, &list)
	if len(list.Sessions) != 2 {
		t.Fatalf("%d sessions, want 2: %+v", len(list.Sessions), list.Sessions)
	}

	if _, err := a.db.Exec(t.Context(), `UPDATE user_sessions SET last_active_at = NOW() - INTERVAL '8 days'
		WHERE id = (SELECT id FROM user_sessions ORDER BY created_at DESC LIMIT 1)`); err != nil {
		t.Fatal(err)
	}
	signedOut := 0
	for _, c := range []*client{admin, phone} {
		if c.get("/api/auth/me").status == http.StatusUnauthorized {
			signedOut++
		}
	}
	if signedOut != 1 {
		t.Fatalf("%d devices signed out by inactivity, want exactly the idle one", signedOut)
	}
}
