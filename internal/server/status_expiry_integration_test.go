//go:build integration

package server

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/msch128/mnema-talk/internal/events"
)

type statusProfile struct {
	StatusText      string     `json:"status_text"`
	StatusExpiresAt *time.Time `json:"status_expires_at"`
}

func setStatusUntil(t *testing.T, c *client, text string, at time.Time) response {
	t.Helper()
	return c.put("/api/users/me/status", map[string]any{"status_text": text, "expires_at": at.UTC().Format(time.RFC3339)})
}

func TestStatusExpiryIsValidatedAndStored(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	max := a.register(admin, "max")

	for name, at := range map[string]time.Time{
		"past":      time.Now().Add(-time.Minute),
		"too far":   time.Now().Add(400 * 24 * time.Hour),
		"just now":  time.Now(),
		"zero time": {},
	} {
		if res := setStatusUntil(t, max, "Lunch", at); res.status != http.StatusBadRequest {
			t.Errorf("%s accepted: %d %s", name, res.status, res.body)
		}
	}

	until := time.Now().Add(time.Hour).Truncate(time.Second)
	res := setStatusUntil(t, max, "Lunch", until)
	if res.status != http.StatusOK {
		t.Fatalf("set with expiry: %d %s", res.status, res.body)
	}
	var p statusProfile
	res.decode(t, &p)
	if p.StatusText != "Lunch" || p.StatusExpiresAt == nil || !p.StatusExpiresAt.Equal(until) {
		t.Fatalf("stored %+v, want Lunch until %s", p, until)
	}

	// Setting a new status without expiry drops the old expiry; clearing ignores one.
	var back, cleared statusProfile
	max.put("/api/users/me/status", map[string]string{"status_text": "Back"}).decode(t, &back)
	if back.StatusExpiresAt != nil {
		t.Fatalf("expiry kept: %+v", back)
	}
	setStatusUntil(t, max, "", until).decode(t, &cleared)
	if cleared.StatusText != "" || cleared.StatusExpiresAt != nil {
		t.Fatalf("clearing kept an expiry: %+v", cleared)
	}
}

func TestExpiredStatusReadsEmptyAndTheSweepClearsAndBroadcasts(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	max := a.register(admin, "max")
	if res := setStatusUntil(t, max, "Lunch", time.Now().Add(time.Hour)); res.status != http.StatusOK {
		t.Fatalf("set: %d %s", res.status, res.body)
	}
	status := func(c *client) string {
		var members []profile
		c.get("/api/members").decode(t, &members)
		for _, m := range members {
			if m.Username == "max" {
				return m.StatusText
			}
		}
		return "<missing>"
	}
	if got := status(admin); got != "Lunch" {
		t.Fatalf("before expiry: %q", got)
	}

	ctx := context.Background()
	if _, err := a.db.Exec(ctx, `UPDATE users SET status_expires_at = NOW() - INTERVAL '1 minute' WHERE username = 'max'`); err != nil {
		t.Fatal(err)
	}
	// Lazy on read: gone before the sweep has run, for others and for the member.
	if got := status(admin); got != "" {
		t.Fatalf("expired status still shown to others: %q", got)
	}
	var me statusProfile
	max.get("/api/auth/me").decode(t, &me)
	if me.StatusText != "" || me.StatusExpiresAt != nil {
		t.Fatalf("expired status still shown to the member: %+v", me)
	}

	before := len(a.events.Snapshot())
	a.router.accounts.ExpireStatuses(ctx)
	var text string
	var at *time.Time
	if err := a.db.QueryRow(ctx, `SELECT status_text, status_expires_at FROM users WHERE username = 'max'`).Scan(&text, &at); err != nil {
		t.Fatal(err)
	}
	if text != "" || at != nil {
		t.Fatalf("row after sweep: %q %v", text, at)
	}
	var updates int
	for _, e := range a.events.Snapshot()[before:] {
		if e.Type == events.UserUpdate {
			updates++
		}
	}
	if updates != 1 {
		t.Fatalf("user_update broadcasts after the sweep: %d, want 1", updates)
	}
	a.router.accounts.ExpireStatuses(ctx)
	if len(a.events.Snapshot()) != before+1 {
		t.Fatal("a second sweep announced again")
	}
}

func TestAdminSettingAStatusIgnoresExpiryAndClearsIt(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	max := a.register(admin, "max")
	setStatusUntil(t, max, "Lunch", time.Now().Add(time.Hour))
	res := admin.put("/api/admin/users/"+max.user.ID.String()+"/status", map[string]string{"status_text": ""})
	if res.status != http.StatusOK {
		t.Fatalf("admin clear: %d %s", res.status, res.body)
	}
	var at *time.Time
	if err := a.db.QueryRow(context.Background(), `SELECT status_expires_at FROM users WHERE username = 'max'`).Scan(&at); err != nil || at != nil {
		t.Fatalf("expiry after admin clear: %v %v", at, err)
	}
}
