//go:build integration

package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat"
)

type dmMember struct {
	ID       uuid.UUID `json:"id"`
	Username string    `json:"username"`
}

type dmView struct {
	ID           uuid.UUID  `json:"id"`
	Group        bool       `json:"group"`
	Members      []dmMember `json:"members"`
	UnreadCount  int        `json:"unread_count"`
	MentionCount int        `json:"mention_count"`
	CanSend      bool       `json:"can_send"`
}

type errorBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func (c *client) startDM(others ...*client) response {
	ids := make([]uuid.UUID, len(others))
	for i, o := range others {
		ids[i] = o.user.ID
	}
	return c.post("/api/dms", map[string]any{"user_ids": ids})
}

// mustDM starts (or reopens) a conversation and returns it.
func (c *client) mustDM(t *testing.T, others ...*client) dmView {
	t.Helper()
	res := c.startDM(others...)
	if res.status != http.StatusCreated && res.status != http.StatusOK {
		t.Fatalf("start direct message: %d %s", res.status, res.body)
	}
	var dm dmView
	res.decode(t, &dm)
	return dm
}

func (c *client) dmPost(dm uuid.UUID, content string) response {
	return c.post("/api/channels/"+dm.String()+"/messages", map[string]any{"content": content})
}

func (r response) errorBody(t *testing.T) errorBody {
	t.Helper()
	var e errorBody
	r.decode(t, &e)
	return e
}

type dmFixture struct {
	a                       *app
	admin                   *client
	alice, bob, carol, dave *client
}

func newDMFixture(t *testing.T, withHub bool) *dmFixture {
	t.Helper()
	a := newApp(t, withHub)
	admin := a.seedAdmin()
	return &dmFixture{a: a, admin: admin,
		alice: a.register(admin, "alice"), bob: a.register(admin, "bobby"), carol: a.register(admin, "carol"), dave: a.register(admin, "davey")}
}

func (f *dmFixture) setDirectMessages(t *testing.T, on bool) {
	t.Helper()
	wantStatus(t, "switch", f.admin.put("/api/admin/onboarding", map[string]any{
		"community_name": "Test Home", "welcome_text": "", "rules_text": "", "direct_messages_enabled": on,
	}), http.StatusOK)
}

func TestADirectMessageIsPrivateToItsMembersTheOwnerIncluded(t *testing.T) {
	f := newDMFixture(t, false)
	dm := f.alice.mustDM(t, f.bob)
	if dm.Group || len(dm.Members) != 2 || !dm.CanSend {
		t.Fatalf("conversation: %+v", dm)
	}
	if again := f.bob.startDM(f.alice); again.status != http.StatusOK {
		t.Fatalf("the pair has one conversation, whoever opens it: %d %s", again.status, again.body)
	}
	res := f.alice.dmPost(dm.ID, "private words")
	wantStatus(t, "alice writes", res, http.StatusCreated)
	var msg struct {
		ID     uuid.UUID `json:"id"`
		Number int64     `json:"number"`
	}
	res.decode(t, &msg)
	if msg.Number >= 0 {
		t.Errorf("a conversation's messages are numbered apart from the public ones: %d", msg.Number)
	}
	wantStatus(t, "bob reads", f.bob.get("/api/channels/"+dm.ID.String()+"/messages"), http.StatusOK)
	if got := f.bob.mustDM(t, f.alice); got.ID != dm.ID {
		t.Fatalf("bob's view of the pair: %+v", got)
	}

	base := "/api/channels/" + dm.ID.String()
	for name, c := range map[string]*client{"another member": f.carol, "the Owner": f.admin} {
		for what, got := range map[string]response{
			"conversation": c.get("/api/dms/" + dm.ID.String()),
			"messages":     c.get(base + "/messages"),
			"write":        c.dmPost(dm.ID, "intruder"),
			"react":        c.post("/api/messages/"+msg.ID.String()+"/reactions", map[string]string{"emoji": "👍"}),
			"thread":       c.get("/api/messages/" + msg.ID.String() + "/thread"),
			"pins":         c.get(base + "/pins"),
			"mark read":    c.post(base+"/read", nil),
			"delete":       c.delete(base + "/messages/" + msg.ID.String()),
			"edit":         c.put(base+"/messages/"+msg.ID.String(), map[string]any{"content": "mine now"}),
			"edit history": c.get(base + "/messages/" + msg.ID.String() + "/edits"),
		} {
			if got.status != http.StatusNotFound {
				t.Errorf("%s: %s answers %d %s, want 404", name, what, got.status, got.body)
			}
		}
		var list []dmView
		c.get("/api/dms").decode(t, &list)
		if len(list) != 0 {
			t.Errorf("%s lists %d conversations", name, len(list))
		}
	}
	if got := f.alice.get(base + "/messages"); got.status != http.StatusOK {
		t.Fatalf("alice reads: %d", got.status)
	}
	// Not a channel anywhere: lists, admin routes and counters know nothing of it.
	for name, c := range map[string]*client{"alice": f.alice, "bob": f.bob, "the Owner": f.admin} {
		if body := string(c.get("/api/channels").body); strings.Contains(body, dm.ID.String()) {
			t.Errorf("%s sees the conversation among the channels: %s", name, body)
		}
	}
	for name, res := range map[string]response{
		"rename":    f.admin.patch("/api/admin/channels/"+dm.ID.String(), map[string]any{"name": "mine"}),
		"delete":    f.admin.delete("/api/admin/channels/" + dm.ID.String()),
		"duplicate": f.admin.post("/api/admin/channels/"+dm.ID.String()+"/duplicate", nil),
		"overwrite": putOverwrites(f.admin, "channels", dm.ID),
		"webhook":   f.admin.post("/api/admin/channels/"+dm.ID.String()+"/webhooks", map[string]any{"name": "spy"}),
	} {
		if res.status != http.StatusNotFound {
			t.Errorf("admin %s: %d %s, want 404", name, res.status, res.body)
		}
	}
	var members []struct {
		ID           uuid.UUID `json:"id"`
		MessageCount int64     `json:"message_count"`
	}
	f.admin.get("/api/members").decode(t, &members)
	for _, m := range members {
		if m.ID == f.alice.user.ID && m.MessageCount != 0 {
			t.Errorf("a member's public message count includes private messages: %d", m.MessageCount)
		}
	}
	// Reads, mentions and the unread summary of channels leave conversations out.
	f.alice.dmPost(dm.ID, "hello @bobby")
	if body := string(f.bob.get("/api/read-state").body); strings.Contains(body, dm.ID.String()) {
		t.Errorf("read-state of channels lists the conversation: %s", body)
	}
	if body := string(f.bob.get("/api/mentions").body); strings.Contains(body, "hello") {
		t.Errorf("the mentions inbox shows a conversation: %s", body)
	}
	var mine []dmView
	f.bob.get("/api/dms").decode(t, &mine)
	if len(mine) != 1 || mine[0].UnreadCount != 2 || mine[0].MentionCount != 1 {
		t.Errorf("bob's list: %+v", mine)
	}
}

func TestEveryRefusalToStartADirectMessageLooksTheSame(t *testing.T) {
	f := newDMFixture(t, false)
	zed := f.a.register(f.admin, "zed")
	wantStatus(t, "carol closes", f.carol.put("/api/users/me/privacy", map[string]any{"dm_policy": "nobody"}), http.StatusOK)
	wantStatus(t, "disable dave", f.admin.post("/api/admin/users/"+f.dave.user.ID.String()+"/disable", nil), http.StatusNoContent)
	if _, err := f.a.db.Exec(context.Background(), `INSERT INTO users (username, display_name, password_hash, is_bot) VALUES ('robot', 'Robot', 'x', TRUE)`); err != nil {
		t.Fatal(err)
	}
	var robot uuid.UUID
	if err := f.a.db.QueryRow(context.Background(), `SELECT id FROM users WHERE username = 'robot'`).Scan(&robot); err != nil {
		t.Fatal(err)
	}
	// Starting is limited to three tries a minute per member: the twelve
	// tries below come from four members who all blocked zed and whom bob
	// blocked.
	var askers []*client
	for i := range 4 {
		asker := f.a.register(f.admin, fmt.Sprintf("asker%d", i))
		wantStatus(t, "bob blocks", f.bob.put("/api/me/blocks/"+asker.user.ID.String(), nil), http.StatusNoContent)
		wantStatus(t, "asker blocks", asker.put("/api/me/blocks/"+zed.user.ID.String(), nil), http.StatusNoContent)
		askers = append(askers, asker)
	}
	targets := map[string]uuid.UUID{
		"blocked me": f.bob.user.ID, "I blocked": zed.user.ID, "policy nobody": f.carol.user.ID,
		"disabled": f.dave.user.ID, "a bot": robot, "unknown": uuid.New(),
	}
	var first *errorBody
	n := 0
	for name, target := range targets {
		for _, group := range []bool{false, true} {
			asker := askers[n/3]
			n++
			ids := []uuid.UUID{target}
			if group {
				ids = append(ids, f.alice.user.ID)
			}
			res := asker.post("/api/dms", map[string]any{"user_ids": ids})
			if res.status != http.StatusForbidden {
				t.Fatalf("%s (group %v): %d %s", name, group, res.status, res.body)
			}
			e := res.errorBody(t)
			if first == nil {
				first = &e
			} else if e != *first {
				t.Errorf("%s answers %+v, others answer %+v", name, e, *first)
			}
		}
	}
	// Invalid requests are plain 400s.
	asker := f.alice
	for name, ids := range map[string][]uuid.UUID{
		"nobody": {}, "yourself": {asker.user.ID}, "repeats": {f.bob.user.ID, f.bob.user.ID},
	} {
		if res := asker.post("/api/dms", map[string]any{"user_ids": ids}); res.status != http.StatusBadRequest {
			t.Errorf("%s: %d %s", name, res.status, res.body)
		}
	}
}

func TestABlockMakesAConversationReadOnlyInBothDirectionsWithoutSayingWhy(t *testing.T) {
	f := newDMFixture(t, false)
	dm := f.alice.mustDM(t, f.bob)
	wantStatus(t, "alice writes", f.alice.dmPost(dm.ID, "hi"), http.StatusCreated)
	wantStatus(t, "bob answers", f.bob.dmPost(dm.ID, "hey"), http.StatusCreated)

	wantStatus(t, "bob blocks", f.bob.put("/api/me/blocks/"+f.alice.user.ID.String(), nil), http.StatusNoContent)
	blocked := f.alice.dmPost(dm.ID, "still there?")
	blocker := f.bob.dmPost(dm.ID, "no")
	if blocked.status != http.StatusForbidden || blocker.status != http.StatusForbidden || blocked.errorBody(t) != blocker.errorBody(t) {
		t.Fatalf("blocked %d %s, blocker %d %s", blocked.status, blocked.body, blocker.status, blocker.body)
	}
	if e := blocked.errorBody(t); e.Error.Message != "Couldn't send this message." {
		t.Errorf("message %q", e.Error.Message)
	}
	// Everything stays readable, and nothing says the conversation is blocked.
	for _, c := range []*client{f.alice, f.bob} {
		wantStatus(t, "read", c.get("/api/channels/"+dm.ID.String()+"/messages"), http.StatusOK)
	}
	var seen dmView
	f.alice.get("/api/dms/"+dm.ID.String()).decode(t, &seen)
	if !seen.CanSend {
		t.Error("can_send must not reveal a block")
	}
	wantStatus(t, "unblock", f.bob.delete("/api/me/blocks/"+f.alice.user.ID.String()), http.StatusNoContent)
	wantStatus(t, "alice writes again", f.alice.dmPost(dm.ID, "hello again"), http.StatusCreated)
}

func TestTheCommunitySwitchMakesConversationsReadOnly(t *testing.T) {
	f := newDMFixture(t, false)
	dm := f.alice.mustDM(t, f.bob)
	wantStatus(t, "write", f.alice.dmPost(dm.ID, "before"), http.StatusCreated)
	f.setDirectMessages(t, false)

	wantStatus(t, "start", f.alice.startDM(f.carol), http.StatusForbidden)
	wantStatus(t, "write", f.alice.dmPost(dm.ID, "after"), http.StatusForbidden)
	wantStatus(t, "react", f.bob.post("/api/messages/"+f.firstMessage(t, dm.ID).String()+"/reactions", map[string]string{"emoji": "👍"}), http.StatusForbidden)
	wantStatus(t, "read", f.bob.get("/api/channels/"+dm.ID.String()+"/messages"), http.StatusOK)
	var seen dmView
	f.bob.get("/api/dms/"+dm.ID.String()).decode(t, &seen)
	if seen.CanSend {
		t.Error("can_send is false while direct messages are off")
	}
	var perms struct {
		Enabled bool `json:"direct_messages_enabled"`
		Can     bool `json:"can_start_direct_messages"`
	}
	f.bob.get("/api/permissions").decode(t, &perms)
	if perms.Enabled || perms.Can {
		t.Errorf("permissions: %+v", perms)
	}
	// Deleting one's own words stays possible.
	wantStatus(t, "delete own", f.alice.delete("/api/channels/"+dm.ID.String()+"/messages/"+f.firstMessage(t, dm.ID).String()), http.StatusNoContent)
	f.setDirectMessages(t, true)
	wantStatus(t, "write again", f.alice.dmPost(dm.ID, "back"), http.StatusCreated)
}

func (f *dmFixture) firstMessage(t *testing.T, dm uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := f.a.db.QueryRow(context.Background(), `SELECT id FROM messages WHERE channel_id = $1 ORDER BY created_at LIMIT 1`, dm).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestSendDirectMessagesIsAPermissionTimeoutsTake(t *testing.T) {
	f := newDMFixture(t, false)
	dm := f.alice.mustDM(t, f.bob)
	wantStatus(t, "write", f.alice.dmPost(dm.ID, "hello"), http.StatusCreated)

	wantStatus(t, "timeout bob", f.admin.post("/api/admin/users/"+f.bob.user.ID.String()+"/timeout", map[string]any{"duration_seconds": 3600}), http.StatusOK)
	wantStatus(t, "timed out writes", f.bob.dmPost(dm.ID, "answer"), http.StatusForbidden)
	wantStatus(t, "timed out starts", f.bob.startDM(f.carol), http.StatusForbidden)
	wantStatus(t, "timed out reads", f.bob.get("/api/channels/"+dm.ID.String()+"/messages"), http.StatusOK)

	// An administrator can withhold the permission from everybody.
	wantStatus(t, "everyone loses it", f.admin.patch("/api/admin/roles/"+everyoneID(t, f.admin).String(), map[string]any{"permissions": 990}), http.StatusOK)
	wantStatus(t, "no permission writes", f.alice.dmPost(dm.ID, "more"), http.StatusForbidden)
	wantStatus(t, "no permission starts", f.alice.startDM(f.carol), http.StatusForbidden)
	wantStatus(t, "still reads", f.alice.get("/api/channels/"+dm.ID.String()+"/messages"), http.StatusOK)
}

func TestTheFirstMessagesToSomeoneWhoNeverAnsweredAreCapped(t *testing.T) {
	f := newDMFixture(t, false)
	dm := f.alice.mustDM(t, f.bob)
	wantStatus(t, "first", f.alice.dmPost(dm.ID, "1"), http.StatusCreated)
	second := f.alice.dmPost(dm.ID, "2")
	wantStatus(t, "second", second, http.StatusCreated)
	third := f.alice.dmPost(dm.ID, "3")
	wantStatus(t, "third", third, http.StatusTooManyRequests)
	// Deleting messages does not buy more.
	var id uuid.UUID
	second.decodeID(t, &id)
	wantStatus(t, "delete", f.alice.delete("/api/channels/"+dm.ID.String()+"/messages/"+id.String()), http.StatusNoContent)
	wantStatus(t, "still capped", f.alice.dmPost(dm.ID, "4"), http.StatusTooManyRequests)

	wantStatus(t, "bob answers", f.bob.dmPost(dm.ID, "hi!"), http.StatusCreated)
	for i := range 5 {
		wantStatus(t, "free after an answer", f.alice.dmPost(dm.ID, "go on"), http.StatusCreated)
		_ = i
	}
}

func (r response) decodeID(t *testing.T, id *uuid.UUID) {
	t.Helper()
	var m struct {
		ID uuid.UUID `json:"id"`
	}
	r.decode(t, &m)
	*id = m.ID
}

func TestStartingDirectMessagesIsLimited(t *testing.T) {
	f := newDMFixture(t, false)
	others := []*client{f.bob, f.carol, f.dave}
	for _, o := range others {
		wantStatus(t, "start", f.alice.startDM(o), http.StatusCreated)
	}
	// Three tries a minute, successful or not.
	wantStatus(t, "fourth try", f.alice.startDM(f.bob), http.StatusTooManyRequests)

	// And ten new conversations a day, however fast the tries come.
	g := newDMFixture(t, false)
	for i := range chat.MaxNewDMsPerDay {
		if _, err := g.a.db.Exec(context.Background(), `
			WITH c AS (INSERT INTO channels (number, name, type, dm_key, dm_created_by) OVERRIDING SYSTEM VALUE
			           VALUES (nextval('dm_number_seq'), '', 'dm', $2, $1) RETURNING id)
			INSERT INTO dm_members (channel_id, user_id) SELECT id, $1 FROM c`, g.alice.user.ID, uuid.NewString()); err != nil {
			t.Fatal(i, err)
		}
	}
	res := g.alice.startDM(g.bob)
	wantStatus(t, "eleventh today", res, http.StatusTooManyRequests)
	if !strings.Contains(string(res.body), "enough direct messages") {
		t.Errorf("body %s", res.body)
	}
}

func TestAGroupConversationCountsNothingFromSomeoneTheReaderBlocked(t *testing.T) {
	f := newDMFixture(t, false)
	dm := f.alice.mustDM(t, f.bob, f.carol)
	wantStatus(t, "carol writes", f.carol.dmPost(dm.ID, "hey @bobby"), http.StatusCreated)
	wantStatus(t, "bob blocks carol", f.bob.put("/api/me/blocks/"+f.carol.user.ID.String(), nil), http.StatusNoContent)
	wantStatus(t, "alice writes", f.alice.dmPost(dm.ID, "hello"), http.StatusCreated)
	var mine []dmView
	f.bob.get("/api/dms").decode(t, &mine)
	if len(mine) != 1 || mine[0].UnreadCount != 1 || mine[0].MentionCount != 0 {
		t.Errorf("bob's list counts carol: %+v", mine)
	}
}
