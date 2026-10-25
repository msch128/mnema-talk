//go:build integration

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/perm"
)

// moderationFixture is an Owner, a moderator who may time out and ban, and a
// plain member in a text channel and a Talk.
type moderationFixture struct {
	a          *app
	admin, mod *client
	target     *client
	text, talk uuid.UUID
	modRole    uuid.UUID
}

func newModerationFixture(t *testing.T, withHub bool) *moderationFixture {
	a := newApp(t, withHub)
	admin := a.seedAdmin()
	f := &moderationFixture{a: a, admin: admin}
	f.mod = a.register(admin, "moddy")
	f.target = a.register(admin, "troll")
	f.modRole = createRole(t, admin, "moderators", perm.TimeoutMembers|perm.BanMembers|perm.ManageMembers)
	assignRoles(t, admin, f.mod, f.modRole)
	f.text = a.createChannel(admin, "general", "text")
	f.talk = a.createChannel(admin, "Lounge", "voice")
	return f
}

func (f *moderationFixture) timeout(c *client, user *client, seconds int, reason string) response {
	return c.post("/api/admin/users/"+user.user.ID.String()+"/timeout", map[string]any{"duration_seconds": seconds, "reason": reason})
}

func TestTimeoutBlocksEverySurfaceAndEndsByItself(t *testing.T) {
	f := newModerationFixture(t, true)
	a, text := f.a, f.text
	msg := a.send(f.target, text, "before")
	other := a.send(f.admin, text, "someone else")
	if res := f.target.post("/api/messages/"+other.String()+"/reactions", map[string]string{"emoji": "👍"}); res.status != http.StatusOK {
		t.Fatalf("react before: %d %s", res.status, res.body)
	}

	conn, _, err := f.target.dialWS(a.origin())
	if err != nil {
		t.Fatal(err)
	}
	conn.send("voice_join", map[string]any{"channel_id": f.talk})
	if _, ok := conn.expect("voice_state_update", 2*time.Second); !ok {
		t.Fatal("could not join before the timeout")
	}

	res := f.timeout(f.mod, f.target, 5, "cool off")
	if res.status != http.StatusOK {
		t.Fatalf("timeout: %d %s", res.status, res.body)
	}
	var state struct {
		TimeoutUntil time.Time `json:"timeout_until"`
		Reason       string    `json:"reason"`
	}
	res.decode(t, &state)
	if state.Reason != "cool off" || time.Until(state.TimeoutUntil) < 3*time.Second {
		t.Fatalf("state %+v", state)
	}

	// The member is told, and removed from the Talk.
	got := conn.collect(2*time.Second, "moderation_notice", "voice_kicked")
	if n := string(got["moderation_notice"]); !strings.Contains(n, "cool off") || !strings.Contains(n, `"timeout"`) {
		t.Fatalf("no notice: %v", got)
	}
	if k := string(got["voice_kicked"]); !strings.Contains(k, "missing_permission") {
		t.Fatalf("not removed from the Talk: %v", got)
	}
	conn.send("voice_join", map[string]any{"channel_id": f.talk})
	if raw, ok := conn.expect("voice_kicked", 2*time.Second); !ok {
		t.Fatalf("Talk join not refused: %s", raw)
	}

	post := "/api/channels/" + text.String() + "/messages"
	if res := f.target.post(post, map[string]any{"content": "hello"}); res.status != http.StatusForbidden {
		t.Errorf("post: %d", res.status)
	}
	if res := f.target.post(post, map[string]any{"content": "reply", "parent_id": other}); res.status != http.StatusForbidden {
		t.Errorf("reply: %d", res.status)
	}
	if res := f.target.put(post+"/"+msg.String(), map[string]any{"content": "edited"}); res.status != http.StatusForbidden {
		t.Errorf("edit: %d", res.status)
	}
	if res := f.target.post("/api/messages/"+other.String()+"/reactions", map[string]string{"emoji": "🔥"}); res.status != http.StatusForbidden {
		t.Errorf("new reaction: %d", res.status)
	}
	if res := f.target.upload("/api/channels/"+text.String()+"/upload", "file", "a.png", "image/png", []byte("\x89PNG\r\n\x1a\n"), nil); res.status != http.StatusForbidden {
		t.Errorf("upload: %d", res.status)
	}
	// Reading, taking back, and deleting one's own words still work.
	if res := f.target.get(post); res.status != http.StatusOK {
		t.Errorf("read: %d", res.status)
	}
	if res := f.target.post("/api/messages/"+other.String()+"/reactions", map[string]string{"emoji": "👍"}); res.status != http.StatusOK {
		t.Errorf("taking a reaction back: %d", res.status)
	}
	if res := f.target.delete(post + "/" + msg.String()); res.status != http.StatusNoContent {
		t.Errorf("delete own: %d", res.status)
	}
	// Nobody else is affected.
	if res := f.mod.post(post, map[string]any{"content": "mod speaks"}); res.status != http.StatusCreated {
		t.Errorf("moderator post: %d", res.status)
	}

	// The member and everyone else can see the timeout.
	var perms struct {
		TimeoutUntil  *time.Time `json:"timeout_until"`
		TimeoutReason string     `json:"timeout_reason"`
		Permissions   perm.Set   `json:"permissions"`
	}
	f.target.get("/api/permissions").decode(t, &perms)
	if perms.TimeoutUntil == nil || perms.TimeoutReason != "cool off" || perms.Permissions.HasAny(perm.TimeoutMask) || !perms.Permissions.Has(perm.ViewChannel) {
		t.Errorf("permissions %+v", perms)
	}
	var members []struct {
		ID           uuid.UUID  `json:"id"`
		TimeoutUntil *time.Time `json:"timeout_until"`
	}
	f.admin.get("/api/members").decode(t, &members)
	seen := false
	for _, m := range members {
		if m.ID == f.target.user.ID {
			seen = m.TimeoutUntil != nil
		} else if m.TimeoutUntil != nil {
			t.Errorf("%s shown as timed out", m.ID)
		}
	}
	if !seen {
		t.Error("the member list does not show the timeout")
	}

	// It lifts by itself.
	time.Sleep(time.Until(state.TimeoutUntil) + 300*time.Millisecond)
	if res := f.target.post(post, map[string]any{"content": "back"}); res.status != http.StatusCreated {
		t.Fatalf("after the timeout: %d %s", res.status, res.body)
	}
	conn.send("voice_join", map[string]any{"channel_id": f.talk})
	if _, ok := conn.expect("voice_state_update", 2*time.Second); !ok {
		t.Error("cannot join a Talk after the timeout")
	}
}

func TestTimeoutCanBeLiftedAndIsAudited(t *testing.T) {
	f := newModerationFixture(t, false)
	post := "/api/channels/" + f.text.String() + "/messages"
	if res := f.timeout(f.mod, f.target, 3600, "spam"); res.status != http.StatusOK {
		t.Fatal(res.status, string(res.body))
	}
	if res := f.target.post(post, map[string]any{"content": "x"}); res.status != http.StatusForbidden {
		t.Fatal(res.status)
	}
	if res := f.mod.delete("/api/admin/users/" + f.target.user.ID.String() + "/timeout"); res.status != http.StatusNoContent {
		t.Fatal(res.status)
	}
	if res := f.target.post(post, map[string]any{"content": "x"}); res.status != http.StatusCreated {
		t.Fatalf("after lifting: %d", res.status)
	}
	var found, lifted bool
	for _, e := range auditEntries(t, f.admin, "action=moderation&limit=50").Entries {
		switch e.Action {
		case "moderation.timeout":
			found = true
			req, _ := e.Details["request"].(map[string]any)
			if e.ActorName != "moddy" || e.TargetName != "troll" || req["reason"] != "spam" {
				t.Errorf("timeout entry %+v", e)
			}
			if after, _ := e.Details["after"].(map[string]any); after["timeout_until"] == nil {
				t.Errorf("no after state %v", e.Details)
			}
		case "moderation.timeout_remove":
			lifted = true
		}
	}
	if !found || !lifted {
		t.Errorf("audit: timeout %v, lifted %v", found, lifted)
	}
}

func TestModerationRespectsHierarchyAndPermissions(t *testing.T) {
	f := newModerationFixture(t, false)
	ownerID := f.admin.user.ID.String()

	// Nobody can touch the Owner, and nobody themselves.
	if res := f.mod.post("/api/admin/users/"+ownerID+"/timeout", map[string]any{"duration_seconds": 60}); res.status < 400 {
		t.Errorf("timed the Owner out: %d", res.status)
	}
	if res := f.mod.post("/api/admin/users/"+ownerID+"/ban", map[string]any{}); res.status < 400 {
		t.Errorf("banned the Owner: %d", res.status)
	}
	if res := f.timeout(f.mod, f.mod, 60, ""); res.status != http.StatusBadRequest {
		t.Errorf("self timeout: %d", res.status)
	}
	if res := f.mod.post("/api/admin/users/"+f.mod.user.ID.String()+"/ban", map[string]any{}); res.status != http.StatusBadRequest {
		t.Errorf("self ban: %d", res.status)
	}

	// An equal or higher role is out of reach; so is a lower one for someone without the permission.
	peer := f.a.register(f.admin, "peer")
	assignRoles(t, f.admin, peer, f.modRole)
	if res := f.timeout(f.mod, peer, 60, ""); res.status != http.StatusForbidden {
		t.Errorf("peer: %d", res.status)
	}
	if res := f.mod.post("/api/admin/users/"+peer.user.ID.String()+"/ban", map[string]any{}); res.status != http.StatusForbidden {
		t.Errorf("peer ban: %d", res.status)
	}
	timeoutOnly := f.a.register(f.admin, "tonly")
	assignRoles(t, f.admin, timeoutOnly, createRole(t, f.admin, "timeouters", perm.TimeoutMembers))
	if res := f.timeout(timeoutOnly, f.target, 60, ""); res.status != http.StatusOK {
		t.Errorf("timeout-only member: %d", res.status)
	}
	if res := timeoutOnly.post("/api/admin/users/"+f.target.user.ID.String()+"/ban", map[string]any{}); res.status != http.StatusForbidden {
		t.Errorf("a timeout permission is no ban permission: %d", res.status)
	}
	if res := f.target.post("/api/admin/users/"+peer.user.ID.String()+"/timeout", map[string]any{"duration_seconds": 60}); res.status != http.StatusForbidden {
		t.Errorf("plain member: %d", res.status)
	}
	if res := f.target.get("/api/admin/bans"); res.status != http.StatusForbidden {
		t.Errorf("plain member lists bans: %d", res.status)
	}

	// Input checks.
	for name, body := range map[string]map[string]any{
		"too short": {"duration_seconds": 1}, "too long": {"duration_seconds": 29 * 24 * 3600},
		"long reason": {"duration_seconds": 60, "reason": strings.Repeat("x", 201)}, "unknown field": {"duration_seconds": 60, "minutes": 1},
	} {
		if res := f.mod.post("/api/admin/users/"+f.target.user.ID.String()+"/timeout", body); res.status != http.StatusBadRequest {
			t.Errorf("%s: %d", name, res.status)
		}
	}
	if res := f.mod.post("/api/admin/users/"+f.target.user.ID.String()+"/ban", map[string]any{"duration_seconds": 5}); res.status != http.StatusBadRequest {
		t.Errorf("short ban: %d", res.status)
	}
	if res := f.mod.post("/api/admin/users/"+f.target.user.ID.String()+"/ban", map[string]any{"reason": strings.Repeat("x", 201)}); res.status != http.StatusBadRequest {
		t.Errorf("long ban reason: %d", res.status)
	}
	if res := f.mod.post("/api/admin/users/"+uuid.NewString()+"/timeout", map[string]any{"duration_seconds": 60}); res.status != http.StatusNotFound {
		t.Errorf("unknown member: %d", res.status)
	}
	if res := f.mod.delete("/api/admin/users/" + f.target.user.ID.String() + "/ban"); res.status != http.StatusNotFound {
		t.Errorf("unban of an unbanned member: %d", res.status)
	}
}

func TestBanBlocksSessionsLoginAndSocketsAndUnbanRestores(t *testing.T) {
	f := newModerationFixture(t, true)
	a := f.a

	// The troll registered with a reusable invite.
	inv := f.admin.post("/api/admin/invites", map[string]any{})
	var invite struct {
		Code string `json:"code"`
	}
	inv.decode(t, &invite)
	viaInvite := a.anon()
	res := viaInvite.post("/api/auth/register", map[string]string{"username": "outlaw", "password": "member-password-123", "invite_code": invite.Code})
	if res.status != http.StatusCreated {
		t.Fatalf("register: %d %s", res.status, res.body)
	}
	var reg struct{ User struct{ ID uuid.UUID } }
	res.decode(t, &reg)
	outlaw := reg.User.ID.String()
	conn := viaInvite.dialLiveWS(a.origin())

	if res := f.mod.post("/api/admin/users/"+outlaw+"/ban", map[string]any{"reason": "ban evasion"}); res.status != http.StatusNoContent {
		t.Fatalf("ban: %d %s", res.status, res.body)
	}
	if raw, ok := conn.expect("moderation_notice", 2*time.Second); !ok || !strings.Contains(string(raw), "ban evasion") {
		t.Errorf("no notice: %s", raw)
	}

	// The cookie is dead, new logins say why, new sockets are refused.
	if res := viaInvite.get("/api/auth/me"); res.status != http.StatusUnauthorized {
		t.Errorf("old session: %d", res.status)
	}
	login := a.anon().post("/api/auth/login", map[string]string{"username": "outlaw", "password": "member-password-123"})
	if login.status != http.StatusForbidden || !strings.Contains(string(login.body), "banned") || !strings.Contains(string(login.body), "ban evasion") {
		t.Errorf("login: %d %s", login.status, login.body)
	}
	if _, resp, err := viaInvite.dialWS(a.origin()); err == nil {
		t.Error("a banned member opened a socket")
	} else if resp != nil && resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("socket: %d", resp.StatusCode)
	}
	if wrong := a.anon().post("/api/auth/login", map[string]string{"username": "outlaw", "password": "not-the-password"}); wrong.status != http.StatusUnauthorized {
		t.Errorf("a wrong password must not reveal the ban: %d", wrong.status)
	}

	// The moderator may ban but not manage invites, so the shared code lives on.
	again := a.anon().post("/api/auth/register", map[string]string{"username": "outlaw2", "password": "member-password-123", "invite_code": invite.Code})
	if again.status != http.StatusCreated {
		t.Errorf("a ban without Manage invites must not delete the invite: %d %s", again.status, again.body)
	}

	// The list shows it; kicking-style Enable needs the ban permission.
	var bans []struct {
		UserID   uuid.UUID `json:"user_id"`
		Reason   string    `json:"reason"`
		BannedBy string    `json:"banned_by"`
	}
	f.mod.get("/api/admin/bans").decode(t, &bans)
	if len(bans) != 1 || bans[0].UserID.String() != outlaw || bans[0].Reason != "ban evasion" || bans[0].BannedBy != "moddy" {
		t.Fatalf("bans %+v", bans)
	}
	var users []struct {
		ID     uuid.UUID `json:"id"`
		Banned bool      `json:"banned"`
	}
	f.admin.get("/api/admin/users").decode(t, &users)
	flagged := false
	for _, u := range users {
		if u.ID.String() == outlaw {
			flagged = u.Banned
		}
	}
	if !flagged {
		t.Error("the user list does not flag the ban")
	}
	// Enable lifts a disabling, never a ban.
	manager := a.register(f.admin, "manager")
	assignRoles(t, f.admin, manager, createRole(t, f.admin, "managers", perm.ManageMembers))
	if res := manager.post("/api/admin/users/"+outlaw+"/enable", nil); res.status != http.StatusNoContent {
		t.Errorf("enable: %d", res.status)
	}
	if res := a.anon().post("/api/auth/login", map[string]string{"username": "outlaw", "password": "member-password-123"}); res.status != http.StatusForbidden {
		t.Errorf("enable lifted a ban: %d", res.status)
	}

	if res := f.mod.delete("/api/admin/users/" + outlaw + "/ban"); res.status != http.StatusNoContent {
		t.Fatalf("unban: %d %s", res.status, res.body)
	}
	if res := a.anon().post("/api/auth/login", map[string]string{"username": "outlaw", "password": "member-password-123"}); res.status != http.StatusOK {
		t.Errorf("login after unban: %d %s", res.status, res.body)
	}
	f.mod.get("/api/admin/bans").decode(t, &bans)
	if len(bans) != 0 {
		t.Errorf("bans after unban: %+v", bans)
	}
}

func TestBansWithADurationExpire(t *testing.T) {
	f := newModerationFixture(t, false)
	id := f.target.user.ID.String()
	if res := f.mod.post("/api/admin/users/"+id+"/ban", map[string]any{"duration_seconds": 3600, "reason": "cooling", "revoke_invite": false}); res.status != http.StatusNoContent {
		t.Fatalf("ban: %d %s", res.status, res.body)
	}
	ctx := context.Background()
	if _, err := f.a.db.Exec(ctx, `UPDATE users SET banned_until = NOW() - INTERVAL '1 second' WHERE id = $1`, f.target.user.ID); err != nil {
		t.Fatal(err)
	}
	f.a.router.PreviewDeps().Accounts.ExpireBans(ctx)
	if res := f.a.anon().post("/api/auth/login", map[string]string{"username": "troll", "password": "member-password-123"}); res.status != http.StatusOK {
		t.Fatalf("login after expiry: %d %s", res.status, res.body)
	}
	expired := false
	for _, e := range auditEntries(t, f.admin, "action=moderation").Entries {
		expired = expired || e.Action == "moderation.ban_expired"
	}
	if !expired {
		t.Error("expiry not audited")
	}
	// A ban without an end does not expire.
	if res := f.mod.post("/api/admin/users/"+id+"/ban", map[string]any{}); res.status != http.StatusNoContent {
		t.Fatal(res.status)
	}
	f.a.router.PreviewDeps().Accounts.ExpireBans(ctx)
	if res := f.a.anon().post("/api/auth/login", map[string]string{"username": "troll", "password": "member-password-123"}); res.status != http.StatusForbidden {
		t.Fatalf("permanent ban lifted: %d", res.status)
	}
}

// collect gathers the first event of each wanted type, until all arrived or
// the time is up.
func (w *wsConn) collect(within time.Duration, types ...string) map[string]json.RawMessage {
	got := map[string]json.RawMessage{}
	timeout := time.After(within)
	for len(got) < len(types) {
		select {
		case ev, ok := <-w.events:
			if !ok {
				return got
			}
			if slices.Contains(types, ev.Type) {
				if _, seen := got[ev.Type]; !seen {
					got[ev.Type] = ev.Payload
				}
			}
		case <-timeout:
			return got
		}
	}
	return got
}

// Ban and Disable are separate states: lifting one never lifts the other, and
// a ban leaves a running timeout alone.
func TestBanAndDisableDoNotClobberEachOther(t *testing.T) {
	f := newModerationFixture(t, false)
	id := f.target.user.ID.String()
	login := func() int {
		return f.a.anon().post("/api/auth/login", map[string]string{"username": "troll", "password": "member-password-123"}).status
	}
	if res := f.timeout(f.mod, f.target, 3600, "t"); res.status != http.StatusOK {
		t.Fatal(res.status)
	}
	if res := f.mod.post("/api/admin/users/"+id+"/disable", nil); res.status != http.StatusNoContent {
		t.Fatal(res.status)
	}
	if res := f.mod.post("/api/admin/users/"+id+"/ban", map[string]any{"reason": "r"}); res.status != http.StatusNoContent {
		t.Fatal(res.status)
	}
	var timeoutLeft bool
	if err := f.a.db.QueryRow(context.Background(), `SELECT timeout_until > NOW() FROM users WHERE id = $1`, f.target.user.ID).Scan(&timeoutLeft); err != nil || !timeoutLeft {
		t.Fatalf("a ban cleared the timeout: %v %v", timeoutLeft, err)
	}
	// Unbanning leaves the account disabled.
	if res := f.mod.delete("/api/admin/users/" + id + "/ban"); res.status != http.StatusNoContent {
		t.Fatal(res.status)
	}
	if got := login(); got != http.StatusForbidden {
		t.Fatalf("unban re-enabled a disabled account: %d", got)
	}
	// Enabling it brings the account back.
	if res := f.mod.post("/api/admin/users/"+id+"/enable", nil); res.status != http.StatusNoContent {
		t.Fatal(res.status)
	}
	if got := login(); got != http.StatusOK {
		t.Fatalf("enabled account cannot sign in: %d", got)
	}

	// An expiring ban does not enable a disabled account either, and an
	// expired ban stops counting before the job clears it.
	f.mod.post("/api/admin/users/"+id+"/disable", nil)
	f.mod.post("/api/admin/users/"+id+"/ban", map[string]any{"duration_seconds": 3600})
	ctx := context.Background()
	if _, err := f.a.db.Exec(ctx, `UPDATE users SET banned_until = NOW() - INTERVAL '1 second' WHERE id = $1`, f.target.user.ID); err != nil {
		t.Fatal(err)
	}
	f.a.router.PreviewDeps().Accounts.ExpireBans(ctx)
	if got := login(); got != http.StatusForbidden {
		t.Fatalf("expiry re-enabled a disabled account: %d", got)
	}
	f.mod.post("/api/admin/users/"+id+"/enable", nil)
	f.mod.post("/api/admin/users/"+id+"/ban", map[string]any{"duration_seconds": 3600})
	if _, err := f.a.db.Exec(ctx, `UPDATE users SET banned_until = NOW() - INTERVAL '1 second' WHERE id = $1`, f.target.user.ID); err != nil {
		t.Fatal(err)
	}
	if got := login(); got != http.StatusOK {
		t.Fatalf("an expired ban still blocks (before the job ran): %d", got)
	}
}

// Banning revokes the member's invite only for callers who may manage invites.
func TestBanRevokesTheInviteOnlyForInviteManagers(t *testing.T) {
	f := newModerationFixture(t, false)
	a := f.a
	newMember := func(code, name string) string {
		res := a.anon().post("/api/auth/register", map[string]string{"username": name, "password": "member-password-123", "invite_code": code})
		if res.status != http.StatusCreated {
			t.Fatalf("register %s: %d %s", name, res.status, res.body)
		}
		var reg struct{ User struct{ ID uuid.UUID } }
		res.decode(t, &reg)
		return reg.User.ID.String()
	}
	var inv struct {
		Code string `json:"code"`
	}
	f.admin.post("/api/admin/invites", map[string]any{"max_uses": 5}).decode(t, &inv)
	code := inv.Code
	first := newMember(code, "guest-one")

	// Ban-only moderator: the ban works, the Owner's invite survives.
	if res := f.mod.post("/api/admin/users/"+first+"/ban", map[string]any{}); res.status != http.StatusNoContent {
		t.Fatalf("ban: %d %s", res.status, res.body)
	}
	if res := a.anon().get("/api/invites/" + code); !strings.Contains(string(res.body), `"valid":true`) {
		t.Fatalf("the invite did not survive a ban by a moderator without Manage invites: %s", res.body)
	}

	// A moderator who also manages invites revokes it, as before.
	inviter := a.register(f.admin, "inviter")
	assignRoles(t, f.admin, inviter, createRole(t, f.admin, "inviters", perm.BanMembers|perm.CreateInvites))
	second := newMember(code, "guest-two")
	if res := inviter.post("/api/admin/users/"+second+"/ban", map[string]any{}); res.status != http.StatusNoContent {
		t.Fatalf("ban: %d %s", res.status, res.body)
	}
	if res := a.anon().get("/api/invites/" + code); !strings.Contains(string(res.body), `"valid":false`) {
		t.Fatalf("the invite survived a ban by an invite manager: %s", res.body)
	}
}
