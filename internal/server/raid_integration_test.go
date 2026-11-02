//go:build integration

package server

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/msch128/mnema-talk/internal/perm"
)

type raidSettings struct {
	JoinLimit      int `json:"join_limit"`
	JoinedInWindow int `json:"joined_in_window"`
	Lockdown       struct {
		Active       bool `json:"active"`
		PauseInvites bool `json:"pause_invites"`
	} `json:"lockdown"`
}

func putRaid(t *testing.T, c *client, limit, write, react, speak int) response {
	t.Helper()
	return c.put("/api/admin/raid", map[string]int{
		"join_limit": limit, "join_window_minutes": 10,
		"min_age_write_minutes": write, "min_age_react_minutes": react, "min_age_speak_minutes": speak,
	})
}

func getRaid(t *testing.T, c *client) raidSettings {
	t.Helper()
	res := c.get("/api/admin/raid")
	wantStatus(t, "get raid", res, http.StatusOK)
	var s raidSettings
	res.decode(t, &s)
	return s
}

func TestNewMemberWaitsGateWritingAndReactingUntilTheyAreOldEnough(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	newbie := a.register(admin, "newbie")
	vouched := a.register(admin, "vouched")
	deputy := a.register(admin, "deputy")
	assignRoles(t, admin, vouched, createRole(t, admin, "vouchers", perm.BypassNewMemberLimits))
	assignRoles(t, admin, deputy, createRole(t, admin, "deputies", perm.Administrator))
	ch := a.createChannel(admin, "general", "text")
	msg := a.send(admin, ch, "hello")
	say := func(c *client) response {
		return c.post("/api/channels/"+ch.String()+"/messages", map[string]any{"content": "hi"})
	}
	react := func(c *client) response {
		return c.post("/api/messages/"+msg.String()+"/reactions", map[string]string{"emoji": "👍"})
	}

	wantStatus(t, "members cannot set it", putRaid(t, newbie, 0, 5, 5, 0), http.StatusForbidden)
	wantStatus(t, "set", putRaid(t, admin, 0, 5, 20, 0), http.StatusOK)

	res := say(newbie)
	wantStatus(t, "new member writes", res, http.StatusForbidden)
	if !strings.Contains(string(res.body), "new members can write in ") {
		t.Fatalf("body %s", res.body)
	}
	res = react(newbie)
	wantStatus(t, "new member reacts", res, http.StatusForbidden)
	if !strings.Contains(string(res.body), "new members can react in ") {
		t.Fatalf("body %s", res.body)
	}
	var info struct {
		NewMember *struct {
			WriteAt *string `json:"write_at"`
			ReactAt *string `json:"react_at"`
			SpeakAt *string `json:"speak_at"`
		} `json:"new_member"`
	}
	perms := newbie.get("/api/permissions")
	perms.decode(t, &info)
	if info.NewMember == nil || info.NewMember.WriteAt == nil || info.NewMember.ReactAt == nil || info.NewMember.SpeakAt != nil {
		t.Fatalf("new_member %+v", info.NewMember)
	}
	for name, c := range map[string]*client{"bypass role": vouched, "administrator": deputy, "owner": admin} {
		wantStatus(t, name+" writes", say(c), http.StatusCreated)
		wantStatus(t, name+" reacts", react(c), http.StatusOK)
	}

	// Seven minutes in: writing is open, reacting still waits.
	if _, err := a.db.Exec(context.Background(), `UPDATE users SET created_at = NOW() - INTERVAL '7 minutes' WHERE username = 'newbie'`); err != nil {
		t.Fatal(err)
	}
	wantStatus(t, "reload", putRaid(t, admin, 0, 5, 20, 0), http.StatusOK)
	wantStatus(t, "writes after 7 min", say(newbie), http.StatusCreated)
	wantStatus(t, "reacts after 7 min", react(newbie), http.StatusForbidden)

	// Turned off, everything is open at once.
	wantStatus(t, "off", putRaid(t, admin, 0, 0, 0, 0), http.StatusOK)
	wantStatus(t, "reacts when off", react(newbie), http.StatusOK)
}

func TestLockdownStopsSignupsAndOptionallyInvitesButNotMembers(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	member := a.register(admin, "member")
	ch := a.createChannel(admin, "general", "text")
	inv := admin.post("/api/admin/invites", map[string]any{})
	var invite struct{ Code string }
	inv.decode(t, &invite)
	signUp := func(name string) response {
		return a.anon().post("/api/auth/register", map[string]string{"username": name, "password": "member-password-123", "invite_code": invite.Code})
	}

	wantStatus(t, "member cannot lock", member.put("/api/admin/raid/lockdown", map[string]any{"active": true}), http.StatusForbidden)
	wantStatus(t, "bad hours", admin.put("/api/admin/raid/lockdown", map[string]any{"active": true, "hours": 9999}), http.StatusBadRequest)
	wantStatus(t, "lock", admin.put("/api/admin/raid/lockdown", map[string]any{"active": true, "hours": 1}), http.StatusOK)
	if !getRaid(t, admin).Lockdown.Active {
		t.Fatal("lockdown is not active")
	}
	res := signUp("late")
	wantStatus(t, "sign-up in lockdown", res, http.StatusServiceUnavailable)
	if !strings.Contains(string(res.body), "sign-ups are paused") {
		t.Fatalf("body %s", res.body)
	}
	// Invites still work, members are untouched.
	wantStatus(t, "invite during lockdown", admin.post("/api/admin/invites", map[string]any{}), http.StatusCreated)
	wantStatus(t, "member writes", member.post("/api/channels/"+ch.String()+"/messages", map[string]any{"content": "still here"}), http.StatusCreated)

	wantStatus(t, "lock and pause invites", admin.put("/api/admin/raid/lockdown", map[string]any{"active": true, "pause_invites": true}), http.StatusOK)
	wantStatus(t, "invite paused", admin.post("/api/admin/invites", map[string]any{}), http.StatusConflict)

	// It ends by itself when its time is up.
	if _, err := a.db.Exec(context.Background(), `UPDATE instance_settings SET lockdown_until = NOW() - INTERVAL '1 minute'`); err != nil {
		t.Fatal(err)
	}
	if getRaid(t, admin).Lockdown.Active {
		t.Fatal("expired lockdown is still active")
	}
	wantStatus(t, "sign-up after expiry", signUp("late"), http.StatusCreated)

	wantStatus(t, "lock again", admin.put("/api/admin/raid/lockdown", map[string]any{"active": true}), http.StatusOK)
	wantStatus(t, "unlock", admin.put("/api/admin/raid/lockdown", map[string]any{"active": false}), http.StatusOK)
	if s := getRaid(t, admin); s.Lockdown.Active || s.Lockdown.PauseInvites {
		t.Fatalf("after unlock %+v", s)
	}
	wantStatus(t, "invite after unlock", admin.post("/api/admin/invites", map[string]any{}), http.StatusCreated)
}

func TestSignupThrottlePausesTheDoorWhenTheWindowIsFull(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	a.register(admin, "first")
	inv := admin.post("/api/admin/invites", map[string]any{"max_uses": 5})
	var invite struct{ Code string }
	inv.decode(t, &invite)
	signUp := func(name string) response {
		return a.anon().post("/api/auth/register", map[string]string{"username": name, "password": "member-password-123", "invite_code": invite.Code})
	}

	joined := getRaid(t, admin).JoinedInWindow
	if joined < 2 {
		t.Fatalf("joined in window %d", joined)
	}
	wantStatus(t, "limit reached", putRaid(t, admin, joined, 0, 0, 0), http.StatusOK)
	wantStatus(t, "sign-up over the limit", signUp("second"), http.StatusServiceUnavailable)
	wantStatus(t, "one more allowed", putRaid(t, admin, joined+1, 0, 0, 0), http.StatusOK)
	wantStatus(t, "sign-up under the limit", signUp("second"), http.StatusCreated)
	wantStatus(t, "full again", signUp("third"), http.StatusServiceUnavailable)
	// Existing members sign in as ever.
	wantStatus(t, "login", a.anon().post("/api/auth/login", map[string]string{"username": "first", "password": "member-password-123"}), http.StatusOK)
	wantStatus(t, "off", putRaid(t, admin, 0, 0, 0, 0), http.StatusOK)
	wantStatus(t, "sign-up with the throttle off", signUp("third"), http.StatusCreated)
	wantStatus(t, "invalid", putRaid(t, admin, -1, 0, 0, 0), http.StatusBadRequest)
	wantStatus(t, "invalid age", putRaid(t, admin, 0, 99999, 0, 0), http.StatusBadRequest)
}

func TestRaidProtectionChangesAreAudited(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	wantStatus(t, "settings", putRaid(t, admin, 5, 10, 0, 0), http.StatusOK)
	wantStatus(t, "lockdown", admin.put("/api/admin/raid/lockdown", map[string]any{"active": true}), http.StatusOK)
	for _, action := range []string{"settings.raid", "settings.lockdown"} {
		var n int
		if err := a.db.QueryRow(context.Background(), `SELECT COUNT(*) FROM audit_log WHERE action = $1`, action).Scan(&n); err != nil || n != 1 {
			t.Fatalf("%s audited %d times (%v)", action, n, err)
		}
	}
}
