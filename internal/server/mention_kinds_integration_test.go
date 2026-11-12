//go:build integration

package server

import (
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/perm"
)

// notMentioned is the "kind" of a member the message does not mention.
const notMentioned int16 = -1

// Who a test message reaches and how. Every kind of recipient exists muted
// and not muted (muted+open), so one message checks both counts. The away
// ones are muted and never connected, so @here does not reach them.
const (
	rPlain     = "plain"     // neither named nor in the role
	rRole      = "role"      // holds @Dev
	rNamed     = "named"     // named in the text
	rListed    = "listed"    // named, and listed in a bot's allowed_mentions.users
	rBoth      = "both"      // holds @Dev and is named
	rAwayRole  = "awayrole"  // holds @Dev, offline
	rAwayNamed = "awaynamed" // named, offline
)

// kindText is the message every cell sends after its group mention.
const kindText = " @Dev @namedmuted @namedopen @listedmuted @listedopen @bothmuted @bothopen @awaynamed"

type kindMember struct {
	c     *client
	kind  string
	muted bool
}

type kindFixture struct {
	*botFixture
	dev     uuid.UUID
	members []kindMember
}

func newKindFixture(t *testing.T) *kindFixture {
	t.Helper()
	f := &kindFixture{botFixture: newBotFixture(t, true)}
	f.a.router.Bots.SetLimits(1000, 1000)
	f.a.router.Webhooks.SetLimits(1000, 1000)
	wantStatus(t, "bot may mention everyone", f.owner.patch("/api/admin/roles/"+f.roleID.String(),
		map[string]any{"permissions": perm.SendMessages | perm.MentionEveryone}), http.StatusOK)
	f.dev = mentionableRole(t, f.owner, "Dev", true)
	add := func(kind, suffix string, muted, online bool) {
		m := f.a.quickMember(f.owner, kind+suffix)
		if kind == rRole || kind == rBoth || kind == rAwayRole {
			assignRoles(t, f.owner, m, f.dev)
		}
		if muted {
			wantStatus(t, "mute", m.put("/api/channels/"+f.open.String()+"/notifications", map[string]any{"level": "mute"}), http.StatusNoContent)
		}
		if online {
			// Drain the socket, so the hub never drops it as a slow reader.
			ws := m.dialLiveWS(f.a.origin())
			go func() {
				for ev := range ws.events {
					_ = ev
				}
			}()
		}
		f.members = append(f.members, kindMember{c: m, kind: kind, muted: muted})
	}
	for _, k := range []string{rPlain, rRole, rNamed, rListed, rBoth} {
		add(k, "muted", true, true)
		add(k, "open", false, true)
	}
	add(rAwayRole, "", true, false)
	add(rAwayNamed, "", true, false)
	return f
}

func (f *kindFixture) listed() []string {
	var ids []string
	for _, m := range f.members {
		if m.kind == rListed {
			ids = append(ids, m.c.user.ID.String())
		}
	}
	return ids
}

// storedKind reads the kind message_mentions holds for the member.
func (f *kindFixture) storedKind(t *testing.T, msg, user uuid.UUID) int16 {
	t.Helper()
	var kind int16
	err := f.a.db.QueryRow(t.Context(), `SELECT kind FROM message_mentions WHERE message_id = $1 AND user_id = $2`, msg, user).Scan(&kind)
	if errors.Is(err, pgx.ErrNoRows) {
		return notMentioned
	}
	if err != nil {
		t.Fatal(err)
	}
	return kind
}

// check compares every member with the kind the documents give them, the
// mention badge (a mute counts only name and role, doc/architecture.md) and
// the mentions inbox (any mention), then marks everything read.
func (f *kindFixture) check(t *testing.T, cell string, msg uuid.UUID, want map[string]int16) {
	t.Helper()
	for _, m := range f.members {
		kind := want[m.kind]
		if got := f.storedKind(t, msg, m.c.user.ID); got != kind {
			t.Errorf("%s: %s stored kind %d, want %d", cell, m.c.user.Username, got, kind)
		}
		count := 0
		if kind == core.MentionKindName || kind == core.MentionKindRole || (kind == core.MentionKindGroup && !m.muted) {
			count = 1
		}
		if rs := m.c.readState(t, f.open); rs.MentionCount != count {
			t.Errorf("%s: %s (muted %v) counts %d mentions, want %d", cell, m.c.user.Username, m.muted, rs.MentionCount, count)
		}
		listed := slices.ContainsFunc(inbox(t, m.c, "").Messages, func(x inboxMessage) bool { return x.ID == msg })
		if listed != (kind != notMentioned) {
			t.Errorf("%s: %s inbox lists the message %v, want %v", cell, m.c.user.Username, listed, kind != notMentioned)
		}
		wantStatus(t, "read all", m.c.post("/api/read-state/read-all", nil), http.StatusNoContent)
	}
}

func (f *kindFixture) hookSender(t *testing.T, name string, settings map[string]any, parse []string) func(string) uuid.UUID {
	t.Helper()
	h := makeHook(t, f.owner, f.open, name)
	wantStatus(t, "settings", f.owner.patch("/api/admin/webhooks/"+h.ID.String(), settings), http.StatusOK)
	return func(content string) uuid.UUID {
		t.Helper()
		res := f.a.exec(h.path+"?wait=true", map[string]any{"content": content, "allowed_mentions": map[string]any{"parse": parse}}, nil)
		wantStatus(t, "execute", res, http.StatusOK)
		var m core.Message
		res.decode(t, &m)
		return m.ID
	}
}

func (f *kindFixture) botSender(t *testing.T) func(string) uuid.UUID {
	t.Helper()
	am := map[string]any{"parse": []string{"everyone"}, "users": f.listed(), "roles": []string{f.dev.String()}}
	return func(content string) uuid.UUID {
		t.Helper()
		res := f.client.post(botPath(f.open, "/messages"), map[string]any{"content": content, "allowed_mentions": am})
		wantStatus(t, "bot send", res, http.StatusCreated)
		var m core.Message
		res.decode(t, &m)
		return m.ID
	}
}

// kinds builds the expected kinds of one cell from a compact row: plain, role,
// named, listed, both, awayrole, awaynamed.
func kinds(row string) map[string]int16 {
	of := map[byte]int16{'N': core.MentionKindName, 'G': core.MentionKindGroup, 'R': core.MentionKindRole, '-': notMentioned}
	order := []string{rPlain, rRole, rNamed, rListed, rBoth, rAwayRole, rAwayNamed}
	cells := strings.Fields(row)
	out := make(map[string]int16, len(order))
	for i, who := range order {
		out[who] = of[cells[i][0]]
	}
	return out
}

// TestMentionKindsOfEveryRouteAndGroup sends "@all/@here @Dev @names" through
// every route that posts and checks each recipient's stored kind, badge
// (muted and not) and inbox. A member reached several ways is stored by the
// strongest reason the sender may use: a name, then a role, then the group.
// doc/architecture.md: a mute lets through a name or a role, not @all/@here;
// doc/webhooks.md: allow_user_mentions off "turns every member mention into
// plain text"; doc/bots.md: a bot's names notify only through parse "users"
// or allowed_mentions.users.
func TestMentionKindsOfEveryRouteAndGroup(t *testing.T) {
	f := newKindFixture(t)
	member := func(content string) uuid.UUID { return postedMessage(t, f.owner, f.open, content).ID }
	usersOff := f.hookSender(t, "users-off", map[string]any{"allow_user_mentions": false, "allow_everyone": true}, []string{"everyone", "roles"})
	everyoneOff := f.hookSender(t, "everyone-off", map[string]any{"allow_user_mentions": true, "allow_everyone": false}, []string{"everyone", "users", "roles"})
	bot := f.botSender(t)

	cells := []struct {
		route string
		send  func(string) uuid.UUID
		group string
		row   string // plain role named listed both awayrole awaynamed
	}{
		{"member", member, "@all", "G R N N N R N"},
		// "@here @Dev": the offline role member is reached by the role alone.
		{"member", member, "@here", "G R N N N R N"},
		{"webhook users off", usersOff, "@all", "G R G G R R G"},
		{"webhook users off", usersOff, "@here", "G R G G R R -"},
		{"webhook everyone off", everyoneOff, "@all", "- R N N N R N"},
		{"webhook everyone off", everyoneOff, "@here", "- R N N N R N"},
		{"bot with lists", bot, "@all", "G R G N R R G"},
		{"bot with lists", bot, "@here", "G R G N R R -"},
	}
	for _, c := range cells {
		cell := c.route + " " + c.group
		f.check(t, cell, c.send(c.group+kindText), kinds(c.row))
	}
}

// TestEditedMentionsGetTheSameKinds: an edit resolves the mentions again
// under the editor's policy, so the kinds follow the same rule. A bot's edit
// takes no allowed_mentions and so notifies only the members it names
// (doc/bots.md).
func TestEditedMentionsGetTheSameKinds(t *testing.T) {
	f := newKindFixture(t)

	draft := postedMessage(t, f.owner, f.open, "draft").ID
	res := f.owner.put("/api/channels/"+f.open.String()+"/messages/"+draft.String(), map[string]any{"content": "@all" + kindText})
	wantStatus(t, "edit", res, http.StatusOK)
	f.check(t, "member edit @all", draft, kinds("G R N N N R N"))

	sent := f.botSender(t)("draft")
	res = f.client.patch(botPath(f.open, "/messages/"+sent.String()), map[string]any{"content": "@all" + kindText})
	wantStatus(t, "bot edit", res, http.StatusOK)
	f.check(t, "bot edit @all", sent, kinds("- - N N N - N"))
}

// TestPushFollowsTheStoredMentionKinds: push decides a mute from the stored
// kinds (push namedMentions reads message_mentions), so it follows the same
// rule as the badge. An open member is pushed every message, which shows when
// the push of each message is done.
func TestPushFollowsTheStoredMentionKinds(t *testing.T) {
	a, fake := newPushApp(t, true, nil)
	owner := a.seedAdmin()
	a.router.Webhooks.SetLimits(1000, 1000)
	dev := mentionableRole(t, owner, "Dev", true)
	first, second := a.createChannel(owner, "first", "text"), a.createChannel(owner, "second", "text")
	subscribed := func(name string, muted bool) *client {
		t.Helper()
		m := a.register(owner, name)
		for _, ch := range []uuid.UUID{first, second} {
			if muted {
				wantStatus(t, "mute", m.put("/api/channels/"+ch.String()+"/notifications", map[string]any{"level": "mute"}), http.StatusNoContent)
			}
		}
		wantStatus(t, "subscribe", m.post("/api/push/subscriptions", subscription(t, "/"+name)), http.StatusCreated)
		return m
	}
	assignRoles(t, owner, subscribed("devmuted", true), dev)
	subscribed("namedmuted", true)
	subscribed("watcher", false)
	pushes := func(path string) int {
		n := 0
		for _, got := range fake.received() {
			if got == path {
				n++
			}
		}
		return n
	}
	waitPushes := func(path string, want int) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for pushes(path) < want && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
		if got := pushes(path); got != want {
			t.Fatalf("%s got %d pushes, want %d (all: %v)", path, got, want, fake.received())
		}
	}

	// "@all @Dev": the role addresses the muted role member.
	postedMessage(t, owner, first, "@all @Dev standup")
	waitPushes("/watcher", 1)
	waitPushes("/devmuted", 1)

	// A webhook that may not name members: "@all @namedmuted" reaches the
	// member through @all only, which a mute silences.
	h := makeHook(t, owner, second, "alerts")
	wantStatus(t, "settings", owner.patch("/api/admin/webhooks/"+h.ID.String(),
		map[string]any{"allow_user_mentions": false, "allow_everyone": true}), http.StatusOK)
	res := a.exec(h.path+"?wait=true", map[string]any{"content": "@all @namedmuted", "allowed_mentions": map[string]any{"parse": []string{"everyone"}}}, nil)
	wantStatus(t, "execute", res, http.StatusOK)
	waitPushes("/watcher", 2)
	time.Sleep(200 * time.Millisecond)
	if n := pushes("/namedmuted"); n != 0 {
		t.Fatalf("the muted member was pushed %d times for a plain-text name next to @all", n)
	}
	if n := pushes("/devmuted"); n != 1 {
		t.Fatalf("@all alone pushed the muted role member: %d pushes", n)
	}
}
