//go:build integration

package server

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/slash"
)

// builtinsFixture is an Owner, a moderator (timeout + ban), a plain member and
// a member with a role above the moderator's, in one text channel and a Talk.
type builtinsFixture struct {
	a                          *app
	owner, mod, member, senior *client
	text, talk                 uuid.UUID
}

func newBuiltinsFixture(t *testing.T) *builtinsFixture {
	t.Helper()
	a := newApp(t, true)
	owner := a.seedAdmin()
	f := &builtinsFixture{a: a, owner: owner}
	f.mod = a.register(owner, "moddy")
	f.member = a.register(owner, "troll")
	f.senior = a.register(owner, "senior")
	assignRoles(t, owner, f.mod, createRole(t, owner, "moderators", perm.TimeoutMembers|perm.BanMembers))
	seniorRole := createRole(t, owner, "seniors", perm.ManageMessages)
	assignRoles(t, owner, f.senior, seniorRole)
	f.putOnTop(seniorRole)
	f.text = a.createChannel(owner, "general", "text")
	f.talk = a.createChannel(owner, "Lounge", "voice")
	return f
}

// putOnTop moves a role above every other custom role.
func (f *builtinsFixture) putOnTop(first uuid.UUID) {
	f.a.t.Helper()
	res := f.owner.get("/api/roles")
	wantStatus(f.a.t, "list roles", res, http.StatusOK)
	var roles []perm.Role
	res.decode(f.a.t, &roles)
	sort.Slice(roles, func(i, j int) bool { return roles[i].Position > roles[j].Position })
	order := []uuid.UUID{first}
	for _, r := range roles {
		if r.ID != first && !r.Everyone && !r.Managed {
			order = append(order, r.ID)
		}
	}
	wantStatus(f.a.t, "reorder roles", f.owner.put("/api/admin/roles/order", map[string]any{"role_ids": order}), http.StatusNoContent)
}

func (f *builtinsFixture) run(c *client, name string, options map[string]any) response {
	return f.runIn(c, f.text, name, options)
}

func (f *builtinsFixture) runIn(c *client, channel uuid.UUID, name string, options map[string]any) response {
	return c.post("/api/channels/"+channel.String()+"/interactions", map[string]any{"command_id": slash.ID(name), "options": options})
}

func (f *builtinsFixture) names(c *client) map[string]bool {
	f.a.t.Helper()
	out := map[string]bool{}
	for _, cmd := range pollCommands(f.a.t, c, f.text) {
		if cmd.Builtin {
			out[cmd.Name] = true
		}
	}
	return out
}

func (f *builtinsFixture) lastMessage(channel uuid.UUID) chat.Message {
	f.a.t.Helper()
	res := f.owner.get("/api/channels/" + channel.String() + "/messages?limit=50")
	wantStatus(f.a.t, "history", res, http.StatusOK)
	var msgs []chat.Message
	res.decode(f.a.t, &msgs)
	if len(msgs) == 0 {
		f.a.t.Fatal("channel has no messages")
	}
	return msgs[len(msgs)-1]
}

func (f *builtinsFixture) canLogin(username string) bool {
	return f.a.anon().post("/api/auth/login", map[string]string{"username": username, "password": "member-password-123"}).status == http.StatusOK
}

func TestTheMenuOffersOnlyTheCommandsAMemberMayUse(t *testing.T) {
	f := newBuiltinsFixture(t)
	for _, name := range []string{"me", "shrug", "tableflip", "unflip", "spoiler", "nick", "thread", "poll"} {
		if !f.names(f.member)[name] {
			t.Errorf("a plain member is missing /%s", name)
		}
	}
	plain := f.names(f.member)
	for _, name := range []string{"timeout", "ban", "kick"} {
		if plain[name] {
			t.Errorf("a plain member is offered /%s", name)
		}
	}
	mod := f.names(f.mod)
	if !mod["timeout"] || !mod["ban"] || mod["kick"] {
		t.Errorf("the moderator's menu: %v", mod)
	}
	owner := f.names(f.owner)
	if !owner["timeout"] || !owner["ban"] || !owner["kick"] {
		t.Errorf("the Owner's menu: %v", owner)
	}
}

func TestCraftedModerationCommandsWithoutThePermissionAreRefusedAndChangeNothing(t *testing.T) {
	f := newBuiltinsFixture(t)
	victim := f.a.register(f.owner, "victim")
	target := map[string]any{"member": victim.user.ID}
	withDuration := map[string]any{"member": victim.user.ID, "duration": "1h"}
	for _, tc := range []struct {
		who     *client
		command string
		options map[string]any
	}{
		{f.member, "timeout", withDuration}, {f.member, "ban", target}, {f.member, "kick", target},
		// A timeout or ban permission is no kick permission, and the other way round.
		{f.mod, "kick", target},
	} {
		if res := f.run(tc.who, tc.command, tc.options); res.status < 400 {
			t.Errorf("%s ran /%s: %d %s", tc.who.user.Username, tc.command, res.status, res.body)
		}
	}
	// The victim neither lost the session nor got a sanction.
	if res := victim.post("/api/channels/"+f.text.String()+"/messages", map[string]any{"content": "still here"}); res.status != http.StatusCreated {
		t.Fatalf("the victim was touched: %d %s", res.status, res.body)
	}
	// The same crafted request through the REST routes is refused as well.
	if res := f.member.post("/api/admin/users/"+victim.user.ID.String()+"/ban", map[string]any{}); res.status != http.StatusForbidden {
		t.Errorf("REST ban by a plain member: %d", res.status)
	}
}

func TestSlashModerationKeepsTheRoleHierarchy(t *testing.T) {
	f := newBuiltinsFixture(t)
	peer := f.a.register(f.owner, "peer")
	assignRoles(t, f.owner, peer, f.roleByName(t, "moderators"))
	opts := func(target *client, extra map[string]any) map[string]any {
		out := map[string]any{"member": target.user.ID}
		for k, v := range extra {
			out[k] = v
		}
		return out
	}
	// Someone with a higher role, an equal role and the Owner are out of reach for /ban and /timeout.
	// (The Owner is refused with 400 like on the REST routes, the others with 403.)
	for name, tc := range map[string]struct {
		target *client
		status int
	}{"higher role": {f.senior, http.StatusForbidden}, "equal role": {peer, http.StatusForbidden}, "the Owner": {f.owner, http.StatusBadRequest}} {
		if res := f.run(f.mod, "ban", opts(tc.target, nil)); res.status != tc.status {
			t.Errorf("/ban on %s: %d %s", name, res.status, res.body)
		}
		if res := f.run(f.mod, "timeout", opts(tc.target, map[string]any{"duration": "10m"})); res.status != tc.status {
			t.Errorf("/timeout on %s: %d %s", name, res.status, res.body)
		}
	}
	if res := f.run(f.mod, "ban", opts(f.mod, nil)); res.status != http.StatusBadRequest {
		t.Errorf("/ban on oneself: %d", res.status)
	}
	if !f.canLogin("senior") || !f.canLogin("peer") {
		t.Fatal("a refused /ban still banned someone")
	}
	// A member below the moderator can be banned, and the same services as the route do the work.
	if res := f.run(f.mod, "ban", opts(f.member, map[string]any{"reason": "spam", "duration": "1d"})); res.status != http.StatusOK {
		t.Fatalf("/ban: %d %s", res.status, res.body)
	}
	if f.canLogin("troll") {
		t.Error("the banned member can still sign in")
	}
	var bans []struct {
		UserID uuid.UUID `json:"user_id"`
		Reason string    `json:"reason"`
	}
	f.owner.get("/api/admin/bans").decode(t, &bans)
	if len(bans) != 1 || bans[0].UserID != f.member.user.ID || bans[0].Reason != "spam" {
		t.Errorf("bans: %+v", bans)
	}
	f.expectAudit("moderation.ban", f.mod.user.ID, f.member.user.ID)
}

// roleByName finds a role.
func (f *builtinsFixture) roleByName(t *testing.T, name string) uuid.UUID {
	t.Helper()
	res := f.owner.get("/api/roles")
	var roles []perm.Role
	res.decode(t, &roles)
	for _, r := range roles {
		if r.Name == name {
			return r.ID
		}
	}
	t.Fatal("no role " + name)
	return uuid.Nil
}

func (f *builtinsFixture) expectAudit(action string, actor, target uuid.UUID) audit.Item {
	f.a.t.Helper()
	page := auditEntries(f.a.t, f.owner, "action="+action+"&limit=20")
	for _, e := range page.Entries {
		if e.ActorID != nil && *e.ActorID == actor && e.TargetID == target.String() {
			if e.Details["via"] != "slash" {
				f.a.t.Errorf("%s entry is not marked as made by a slash command: %v", action, e.Details)
			}
			return e
		}
	}
	f.a.t.Fatalf("no %s audit entry by %s for %s: %+v", action, actor, target, page.Entries)
	return audit.Item{}
}

func TestSlashTimeoutIsTheRoutesTimeout(t *testing.T) {
	f := newBuiltinsFixture(t)
	if res := f.run(f.mod, "timeout", map[string]any{"member": f.member.user.ID, "duration": "nonsense"}); res.status != http.StatusBadRequest {
		t.Errorf("a bad duration: %d", res.status)
	}
	if res := f.run(f.mod, "timeout", map[string]any{"member": f.member.user.ID, "duration": "1s"}); res.status != http.StatusBadRequest {
		t.Errorf("a duration below the limit: %d", res.status)
	}
	if res := f.run(f.mod, "timeout", map[string]any{"member": f.member.user.ID, "duration": "10m", "reason": "calm down"}); res.status != http.StatusOK {
		t.Fatalf("/timeout: %d %s", res.status, res.body)
	}
	if res := f.member.post("/api/channels/"+f.text.String()+"/messages", map[string]any{"content": "hi"}); res.status != http.StatusForbidden {
		t.Errorf("a timed out member posted: %d", res.status)
	}
	e := f.expectAudit("moderation.timeout", f.mod.user.ID, f.member.user.ID)
	if e.Details["duration_seconds"] != float64(600) {
		t.Errorf("audit details: %v", e.Details)
	}
}

func TestKickSignsTheMemberOutEverywhereWithoutASanction(t *testing.T) {
	f := newBuiltinsFixture(t)
	a, ctx := f.a, context.Background()
	if _, err := a.db.Exec(ctx, `INSERT INTO push_subscriptions (user_id, endpoint, p256dh, auth) VALUES ($1, 'https://push.example.org/troll', 'k', 'a')`, f.member.user.ID); err != nil {
		t.Fatal(err)
	}
	conn, _, err := f.member.dialWS(a.origin())
	if err != nil {
		t.Fatal(err)
	}
	conn.send("voice_join", map[string]any{"channel_id": f.talk})
	if _, ok := conn.expect("voice_state_update", 2*time.Second); !ok {
		t.Fatal("could not join the Talk")
	}

	// The role hierarchy applies to /kick like to /ban: a role with Kick members cannot kick a higher role.
	kickers := f.a.register(f.owner, "kicker")
	kickRole := createRole(t, f.owner, "kickers", perm.KickMembers)
	assignRoles(t, f.owner, kickers, kickRole)
	f.putOnTop(f.roleByName(t, "seniors"))
	if res := f.run(kickers, "kick", map[string]any{"member": f.senior.user.ID}); res.status != http.StatusForbidden {
		t.Fatalf("kick of a higher role: %d %s", res.status, res.body)
	}
	if res := f.run(kickers, "kick", map[string]any{"member": f.owner.user.ID}); res.status != http.StatusBadRequest {
		t.Errorf("kicked the Owner: %d", res.status)
	}
	if res := f.run(f.owner, "kick", map[string]any{"member": f.owner.user.ID}); res.status != http.StatusBadRequest {
		t.Errorf("kicked oneself: %d", res.status)
	}

	if res := f.run(f.owner, "kick", map[string]any{"member": f.member.user.ID, "reason": "take a break"}); res.status != http.StatusOK {
		t.Fatalf("/kick: %d %s", res.status, res.body)
	}
	got := conn.collect(2*time.Second, "kicked", "voice_kicked")
	if k := string(got["kicked"]); !strings.Contains(k, "take a break") {
		t.Errorf("no kicked event with the reason: %v", got)
	}
	if !conn.expectClosed(3 * time.Second) {
		t.Error("the connection stayed open")
	}
	// Signed out everywhere: the old session is dead, push devices are gone.
	if res := f.member.get("/api/auth/me"); res.status != http.StatusUnauthorized {
		t.Errorf("old session after the kick: %d", res.status)
	}
	var devices int
	if err := a.db.QueryRow(ctx, `SELECT COUNT(*) FROM push_subscriptions WHERE user_id = $1`, f.member.user.ID).Scan(&devices); err != nil || devices != 0 {
		t.Errorf("push devices left: %d (%v)", devices, err)
	}
	// No lasting sanction: they sign in again at once and can post.
	again := a.login("troll", "member-password-123")
	if res := again.post("/api/channels/"+f.text.String()+"/messages", map[string]any{"content": "back"}); res.status != http.StatusCreated {
		t.Errorf("a kicked member cannot post after signing in: %d %s", res.status, res.body)
	}
	var banned, timedOut bool
	if err := a.db.QueryRow(ctx, `SELECT banned_at IS NOT NULL, timeout_until IS NOT NULL FROM users WHERE id = $1`, f.member.user.ID).Scan(&banned, &timedOut); err != nil || banned || timedOut {
		t.Errorf("sanction left behind: banned=%v timeout=%v (%v)", banned, timedOut, err)
	}
	f.expectAudit("moderation.kick", f.owner.user.ID, f.member.user.ID)
}

func TestAnAdministratorRoleKicksByDefault(t *testing.T) {
	f := newBuiltinsFixture(t)
	admin := f.a.register(f.owner, "admin2")
	assignRoles(t, f.owner, admin, createRole(t, f.owner, "admins", perm.Administrator))
	if !f.names(admin)["kick"] {
		t.Fatal("an Administrator is not offered /kick")
	}
	if res := f.run(admin, "kick", map[string]any{"member": f.member.user.ID}); res.status != http.StatusOK {
		t.Fatalf("/kick by an Administrator: %d %s", res.status, res.body)
	}
	if res := f.member.get("/api/auth/me"); res.status != http.StatusUnauthorized {
		t.Errorf("kicked member's session: %d", res.status)
	}
}

func TestNickRenamesThroughTheProfilePathWithItsLimitAndRateLimit(t *testing.T) {
	f := newBuiltinsFixture(t)
	if res := f.member.put("/api/users/me/profile", map[string]any{"display_name": "Troll", "bio": "my bio"}); res.status != http.StatusOK {
		t.Fatalf("profile: %d %s", res.status, res.body)
	}
	if res := f.run(f.member, "nick", map[string]any{"name": strings.Repeat("x", 25)}); res.status != http.StatusBadRequest {
		t.Errorf("a 25 character name: %d %s", res.status, res.body)
	}
	if res := f.run(f.member, "nick", map[string]any{"name": "Grumpy"}); res.status != http.StatusOK {
		t.Fatalf("/nick: %d %s", res.status, res.body)
	}
	var me struct {
		DisplayName string `json:"display_name"`
		Bio         string `json:"bio"`
	}
	f.member.get("/api/users/"+f.member.user.ID.String()).decode(t, &me)
	if me.DisplayName != "Grumpy" || me.Bio != "my bio" {
		t.Errorf("profile after /nick: %+v", me)
	}
	// Someone else's name is never an option: the command only knows its caller.
	if res := f.run(f.member, "nick", map[string]any{"name": "Evil", "member": f.owner.user.ID}); res.status != http.StatusBadRequest {
		t.Errorf("an unknown option: %d", res.status)
	}
	limited := false
	for i := range 5 {
		if res := f.run(f.member, "nick", map[string]any{"name": "Name" + string(rune('A'+i))}); res.status == http.StatusTooManyRequests {
			limited = true
			break
		}
	}
	if !limited {
		t.Error("/nick is not rate limited")
	}
}

func TestTextCommandsPostWhatTheyPromise(t *testing.T) {
	f := newBuiltinsFixture(t)
	for _, tc := range []struct {
		command, text, want string
	}{
		{"me", "waves", "_waves_"},
		{"spoiler", "the butler did it", "||the butler did it||"},
		{"shrug", "dunno", `dunno ¯\\\_(ツ)_/¯`},
		{"shrug", "", `¯\\\_(ツ)_/¯`},
		{"tableflip", "ugh", "ugh (╯°□°)╯︵ ┻━┻"},
		{"unflip", "", `┬─┬ノ( º \_ ºノ)`},
	} {
		opts := map[string]any{}
		if tc.text != "" {
			opts["text"] = tc.text
		}
		res := f.run(f.member, tc.command, opts)
		if res.status != http.StatusOK {
			t.Fatalf("/%s: %d %s", tc.command, res.status, res.body)
		}
		if got := f.lastMessage(f.text); got.Content != tc.want || got.UserID != f.member.user.ID {
			t.Errorf("/%s posted %q by %s, want %q", tc.command, got.Content, got.UserID, tc.want)
		}
	}
	if res := f.run(f.member, "me", map[string]any{}); res.status != http.StatusBadRequest {
		t.Errorf("/me without text: %d", res.status)
	}
	// The wrapped text is validated like any message.
	if res := f.run(f.member, "me", map[string]any{"text": strings.Repeat("x", 4000)}); res.status != http.StatusBadRequest {
		t.Errorf("a text that is too long once wrapped: %d", res.status)
	}
}

func TestThreadStartsOnTheLatestOwnMessage(t *testing.T) {
	f := newBuiltinsFixture(t)
	if res := f.run(f.member, "thread", map[string]any{"title": "Plans"}); res.status != http.StatusBadRequest {
		t.Errorf("/thread without a message of one's own: %d", res.status)
	}
	root := f.a.send(f.member, f.text, "let's meet")
	f.a.send(f.owner, f.text, "later message of someone else")
	res := f.run(f.member, "thread", map[string]any{"title": "Plans for Friday"})
	if res.status != http.StatusOK {
		t.Fatalf("/thread: %d %s", res.status, res.body)
	}
	var out struct {
		MessageID uuid.UUID `json:"message_id"`
	}
	res.decode(t, &out)
	thread := f.member.get("/api/messages/" + root.String() + "/thread")
	wantStatus(t, "thread", thread, http.StatusOK)
	if !strings.Contains(string(thread.body), out.MessageID.String()) || !strings.Contains(string(thread.body), "Plans for Friday") {
		t.Errorf("the title is not the first reply of the thread: %s", thread.body)
	}
}

func TestBuiltinsAreInTheCommandListAndHealthAdvertisesThem(t *testing.T) {
	f := newBuiltinsFixture(t)
	for _, cmd := range pollCommands(t, f.owner, f.text) {
		if cmd.Builtin && cmd.ID != slash.ID(cmd.Name) {
			t.Errorf("/%s has an unstable ID", cmd.Name)
		}
	}
	res := f.a.anon().get("/api/health")
	wantStatus(t, "health", res, http.StatusOK)
	if !strings.Contains(string(res.body), `"slash.builtins"`) {
		t.Errorf("health does not list slash.builtins: %s", res.body)
	}
}

func TestMsgOpensTheDirectMessageUnderTheRulesOfTheRoute(t *testing.T) {
	f := newBuiltinsFixture(t)
	post := func(from, to *client) response {
		return f.run(from, "msg", map[string]any{"member": to.user.ID, "text": "psst"})
	}
	if !f.names(f.member)["msg"] {
		t.Fatal("/msg is not offered")
	}
	res := post(f.member, f.senior)
	wantStatus(t, "/msg", res, http.StatusOK)
	var out struct {
		MessageID uuid.UUID `json:"message_id"`
	}
	res.decode(t, &out)
	var dms []struct {
		ID uuid.UUID `json:"id"`
	}
	f.senior.get("/api/dms").decode(t, &dms)
	if len(dms) != 1 {
		t.Fatalf("conversations of the receiver: %+v", dms)
	}
	history := f.senior.get("/api/channels/" + dms[0].ID.String() + "/messages?limit=10")
	if !strings.Contains(string(history.body), out.MessageID.String()) || !strings.Contains(string(history.body), "psst") {
		t.Errorf("message not in the conversation: %s", history.body)
	}
	wantStatus(t, "/msg again", post(f.member, f.senior), http.StatusOK)
	f.senior.get("/api/dms").decode(t, &dms)
	if len(dms) != 1 {
		t.Errorf("a second conversation was opened: %+v", dms)
	}

	// A refusal about the other member is the route's one text, whatever the reason.
	closed := f.a.register(f.owner, "closed")
	wantStatus(t, "closes", closed.put("/api/users/me/privacy", map[string]any{"dm_policy": "nobody"}), http.StatusOK)
	blocker := f.a.register(f.owner, "blocker")
	wantStatus(t, "blocks", blocker.put("/api/me/blocks/"+f.mod.user.ID.String(), nil), http.StatusNoContent)
	rest := f.mod.post("/api/dms", map[string]any{"user_ids": []uuid.UUID{closed.user.ID}})
	for name, other := range map[string]*client{"dm_policy nobody": closed, "blocked": blocker} {
		got := post(f.mod, other)
		if got.status != http.StatusForbidden || string(got.body) != string(rest.body) {
			t.Errorf("%s: %d %s, the route says %d %s", name, got.status, got.body, rest.status, rest.body)
		}
	}

	// Without Send direct messages the command is neither offered nor run.
	wantStatus(t, "everyone loses it", f.owner.patch("/api/admin/roles/"+everyoneID(t, f.owner).String(), map[string]any{"permissions": 990}), http.StatusOK)
	if f.names(f.senior)["msg"] {
		t.Error("/msg is offered without Send direct messages")
	}
	if got := post(f.senior, f.member); got.status != http.StatusNotFound && got.status != http.StatusForbidden {
		t.Errorf("/msg without the permission: %d %s", got.status, got.body)
	}
}
