//go:build integration

package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
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

// Sign-ups run one after the other under the settings row's lock: of many at
// once, only as many as the limit leaves room for get in.
func TestConcurrentSignupsCannotPassTheLimit(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	inv := admin.post("/api/admin/invites", map[string]any{"max_uses": 20})
	var invite struct{ Code string }
	inv.decode(t, &invite)
	room := 3
	wantStatus(t, "limit", putRaid(t, admin, getRaid(t, admin).JoinedInWindow+room, 0, 0, 0), http.StatusOK)

	var wg sync.WaitGroup
	var mu sync.Mutex
	statuses := map[int]int{}
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			res := a.anon().post("/api/auth/register", map[string]string{"username": fmt.Sprintf("racer%d", i), "password": "member-password-123", "invite_code": invite.Code})
			mu.Lock()
			statuses[res.status]++
			mu.Unlock()
		}(i)
	}
	wg.Wait()
	if statuses[http.StatusCreated] != room || statuses[http.StatusServiceUnavailable] != 10-room {
		t.Fatalf("statuses %v, want %d created", statuses, room)
	}
}

func TestDeletedAccountsStillCountAndALockdownRestartsItsClock(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	a.register(admin, "gone")
	before := getRaid(t, admin).JoinedInWindow
	if _, err := a.db.Exec(context.Background(), `UPDATE users SET deleted_at = NOW() WHERE username = 'gone'`); err != nil {
		t.Fatal(err)
	}
	if after := getRaid(t, admin).JoinedInWindow; after != before {
		t.Fatalf("deleting an account changed the count from %d to %d", before, after)
	}

	wantStatus(t, "lock", admin.put("/api/admin/raid/lockdown", map[string]any{"active": true, "hours": 1}), http.StatusOK)
	if _, err := a.db.Exec(context.Background(), `UPDATE instance_settings SET lockdown_started_at = NOW() - INTERVAL '5 days', lockdown_until = NOW() - INTERVAL '1 day'`); err != nil {
		t.Fatal(err)
	}
	wantStatus(t, "lock again after expiry", admin.put("/api/admin/raid/lockdown", map[string]any{"active": true}), http.StatusOK)
	var fresh bool
	if err := a.db.QueryRow(context.Background(), `SELECT lockdown_started_at > NOW() - INTERVAL '1 minute' FROM instance_settings`).Scan(&fresh); err != nil || !fresh {
		t.Fatalf("started_at was not restarted (%v, %v)", fresh, err)
	}
}

func startDM(t *testing.T, c *client, with ...*client) response {
	t.Helper()
	ids := make([]string, 0, len(with))
	for _, w := range with {
		ids = append(ids, w.user.ID.String())
	}
	return c.post("/api/dms", map[string]any{"user_ids": ids})
}

func dmID(t *testing.T, res response) string {
	t.Helper()
	var dm struct{ ID string }
	res.decode(t, &dm)
	return dm.ID
}

// While the write wait runs a new member may answer in a conversation but
// neither start one nor write first (DM spam is the classic raid vector);
// reactions follow the react wait.
func TestDirectMessagesFollowTheWaitsOfANewMember(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	newbie := a.register(admin, "newbie")
	friend := a.register(admin, "friend")
	other := a.register(admin, "other")
	for _, name := range []string{"friend", "other"} {
		if _, err := a.db.Exec(context.Background(), `UPDATE users SET created_at = NOW() - INTERVAL '30 days' WHERE username = $1`, name); err != nil {
			t.Fatal(err)
		}
	}
	wantStatus(t, "set", putRaid(t, admin, 0, 60, 60, 0), http.StatusOK)
	say := func(c *client, dm, text string) response {
		return c.post("/api/channels/"+dm+"/messages", map[string]any{"content": text})
	}

	// 1. Starting a conversation (or a group) is refused with the seconds left.
	res := startDM(t, newbie, friend)
	wantStatus(t, "new member starts a DM", res, http.StatusForbidden)
	if !strings.Contains(string(res.body), "new members can write in ") {
		t.Fatalf("body %s", res.body)
	}
	wantStatus(t, "new member starts a group", startDM(t, newbie, friend, other), http.StatusForbidden)
	var info struct {
		CanStart bool `json:"can_start_direct_messages"`
	}
	newbie.get("/api/permissions").decode(t, &info)
	if info.CanStart {
		t.Fatal("permissions still offer starting a conversation")
	}

	// 2. Answering someone who wrote first is allowed.
	written := startDM(t, friend, newbie)
	wantStatus(t, "friend starts", written, http.StatusCreated)
	withMessage := dmID(t, written)
	wantStatus(t, "friend writes first", say(friend, withMessage, "hi"), http.StatusCreated)
	wantStatus(t, "new member answers", say(newbie, withMessage, "hello"), http.StatusCreated)

	// 3. Writing first in a conversation that is still empty is refused.
	empty := startDM(t, other, newbie)
	wantStatus(t, "other starts", empty, http.StatusCreated)
	emptyID := dmID(t, empty)
	res = say(newbie, emptyID, "first!")
	wantStatus(t, "new member writes first", res, http.StatusForbidden)
	if !strings.Contains(string(res.body), "new members can write in ") {
		t.Fatalf("body %s", res.body)
	}
	wantStatus(t, "once the other wrote, answering is fine", say(other, emptyID, "hey"), http.StatusCreated)
	wantStatus(t, "new member answers there too", say(newbie, emptyID, "hey you"), http.StatusCreated)

	// 4. Reactions in a conversation follow the react wait.
	var page []struct{ ID string }
	friendMsgs := newbie.get("/api/channels/" + withMessage + "/messages")
	friendMsgs.decode(t, &page)
	if len(page) == 0 {
		t.Fatalf("no messages: %s", friendMsgs.body)
	}
	res = newbie.post("/api/messages/"+page[0].ID+"/reactions", map[string]string{"emoji": "👍"})
	wantStatus(t, "new member reacts in a DM", res, http.StatusForbidden)
	if !strings.Contains(string(res.body), "new members can react in ") {
		t.Fatalf("body %s", res.body)
	}
	wantStatus(t, "an older member reacts", friend.post("/api/messages/"+page[0].ID+"/reactions", map[string]string{"emoji": "👍"}), http.StatusOK)

	// Once the waits are over the member starts a conversation like anyone.
	wantStatus(t, "off", putRaid(t, admin, 0, 0, 0, 0), http.StatusOK)
	wantStatus(t, "starts after the wait", startDM(t, newbie, friend), http.StatusOK)
}

// Camera and screen share are held with the microphone.
func TestTheSpeakWaitAlsoHoldsVideoInThePermissions(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	newbie := a.register(admin, "newbie")
	wantStatus(t, "set", putRaid(t, admin, 0, 0, 0, 60), http.StatusOK)
	var info struct {
		Permissions int64 `json:"permissions"`
	}
	newbie.get("/api/permissions").decode(t, &info)
	if info.Permissions&int64(perm.Speak|perm.Video) != 0 {
		t.Fatalf("permissions %b still hold Speak or Video", info.Permissions)
	}
}
