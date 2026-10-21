//go:build integration

package server

import (
	"net/http"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/perm"
)

type inboxMessage struct {
	ID           uuid.UUID   `json:"id"`
	ChannelID    uuid.UUID   `json:"channel_id"`
	Content      string      `json:"content"`
	Mentions     []uuid.UUID `json:"mentions"`
	MentionRoles []uuid.UUID `json:"mention_roles"`
}

type inboxPage struct {
	Messages []inboxMessage `json:"messages"`
	HasMore  bool           `json:"has_more"`
}

func inbox(t *testing.T, c *client, query string) inboxPage {
	t.Helper()
	res := c.get("/api/mentions" + query)
	if res.status != http.StatusOK {
		t.Fatalf("mentions inbox: %d %s", res.status, res.body)
	}
	var page inboxPage
	res.decode(t, &page)
	return page
}

func postedMessage(t *testing.T, c *client, ch uuid.UUID, content string) inboxMessage {
	t.Helper()
	res := c.post("/api/channels/"+ch.String()+"/messages", map[string]any{"content": content})
	wantStatus(t, "post "+content, res, http.StatusCreated)
	var m inboxMessage
	res.decode(t, &m)
	return m
}

func TestRoleMentionsFollowTheAuthorsRights(t *testing.T) {
	f := newAccessFixture(t, false)
	ch := f.a.createChannel(f.owner, "general", "text")
	team := createRole(t, f.owner, "Dev Team", 0)
	member := f.a.register(f.owner, "member")
	assignRoles(t, f.owner, member, team)
	// Plain members lose the right to mention everyone; Staff keeps it.
	wantStatus(t, "deny", putOverwrites(f.owner, "channels", ch,
		overwrite("role", f.everyone, 0, perm.MentionEveryone),
		overwrite("role", f.staff, perm.MentionEveryone, 0)), http.StatusNoContent)

	// Not mentionable and the author may not mention everyone: plain text.
	m := postedMessage(t, f.outsider, ch, "hello @dev team")
	if len(m.MentionRoles) != 0 || member.readState(t, ch).MentionCount != 0 {
		t.Fatalf("a non-mentionable role notified: %+v", m)
	}

	// Mentionable: anyone may use it, case-insensitively, also with a space.
	wantStatus(t, "mentionable", f.owner.patch("/api/admin/roles/"+team.String(), map[string]any{"mentionable": true}), http.StatusOK)
	m = postedMessage(t, f.outsider, ch, "ping @DEV team, please")
	if !slices.Equal(m.MentionRoles, []uuid.UUID{team}) {
		t.Fatalf("role not recorded: %+v", m)
	}
	if rs := member.readState(t, ch); rs.MentionCount != 1 {
		t.Fatalf("role member not notified: %+v", rs)
	}
	if rs := f.insider.readState(t, ch); rs.MentionCount != 0 {
		t.Fatalf("a non-member was notified: %+v", rs)
	}

	// Editing re-resolves: the mention goes away with the text.
	wantStatus(t, "edit", f.outsider.put("/api/channels/"+ch.String()+"/messages/"+m.ID.String(), map[string]string{"content": "never mind"}), http.StatusOK)
	if rs := member.readState(t, ch); rs.MentionCount != 0 {
		t.Fatalf("edit left the mention: %+v", rs)
	}

	// An author holding Mention everyone may mention a role that is not mentionable.
	wantStatus(t, "locked", f.owner.patch("/api/admin/roles/"+team.String(), map[string]any{"mentionable": false}), http.StatusOK)
	postedMessage(t, f.insider, ch, "@Dev Team standup")
	if rs := member.readState(t, ch); rs.MentionCount != 1 {
		t.Fatalf("Mention everyone did not reach the role: %+v", rs)
	}
}

func TestRoleMentionNamedUserWinsAndHiddenChannelsDropOut(t *testing.T) {
	f := newAccessFixture(t, false)
	open := f.a.createChannel(f.owner, "open", "text")
	secret := f.privateChannel(t, "staff-only", "text")
	crew := createRole(t, f.owner, "crew", 0)
	wantStatus(t, "mentionable", f.owner.patch("/api/admin/roles/"+crew.String(), map[string]any{"mentionable": true}), http.StatusOK)
	member := f.a.register(f.owner, "member")
	assignRoles(t, f.owner, member, crew)
	namesake := f.a.register(f.owner, "crew")

	// A user named like the role wins: only they are notified.
	postedMessage(t, f.owner, open, "@crew assemble")
	if rs := namesake.readState(t, open); rs.MentionCount != 1 {
		t.Fatalf("the user named crew was not notified: %+v", rs)
	}
	if rs := member.readState(t, open); rs.MentionCount != 0 {
		t.Fatalf("the role members were notified too: %+v", rs)
	}

	// Another role of the same kind, but the channel is hidden from the member.
	ops := createRole(t, f.owner, "ops", 0)
	wantStatus(t, "mentionable", f.owner.patch("/api/admin/roles/"+ops.String(), map[string]any{"mentionable": true}), http.StatusOK)
	assignRoles(t, f.owner, member, crew, ops)
	m := postedMessage(t, f.owner, secret, "@ops secret plan")
	if !slices.Equal(m.MentionRoles, []uuid.UUID{ops}) || len(m.Mentions) != 0 {
		t.Fatalf("a non-viewer was mentioned: %+v", m)
	}
	if got := inbox(t, member, ""); len(got.Messages) != 0 {
		t.Fatalf("inbox shows a hidden channel: %+v", got)
	}
}

func TestMentionsInbox(t *testing.T) {
	f := newAccessFixture(t, false)
	ch := f.a.createChannel(f.owner, "general", "text")
	secret := f.privateChannel(t, "staff-only", "text")
	mine := postedMessage(t, f.outsider, ch, "my own words")

	direct := postedMessage(t, f.owner, ch, "hey @outsider")
	reply := f.owner.post("/api/channels/"+ch.String()+"/messages", map[string]any{"content": "agreed", "reply_to_id": mine.ID})
	wantStatus(t, "reply", reply, http.StatusCreated)
	var replied inboxMessage
	reply.decode(t, &replied)
	postedMessage(t, f.owner, ch, "unrelated")
	postedMessage(t, f.owner, secret, "@outsider are you there")
	postedMessage(t, f.outsider, ch, "@outsider talking to myself")
	all := postedMessage(t, f.owner, ch, "@all news")

	page := inbox(t, f.outsider, "")
	var got []uuid.UUID
	for _, m := range page.Messages {
		got = append(got, m.ID)
		if m.ChannelID == secret {
			t.Fatalf("hidden channel in the inbox: %+v", m)
		}
	}
	if want := []uuid.UUID{all.ID, replied.ID, direct.ID}; !slices.Equal(got, want) || page.HasMore {
		t.Fatalf("inbox = %v (more=%v), want %v", got, page.HasMore, want)
	}

	// Paging walks back through the same list.
	first := inbox(t, f.outsider, "?limit=2")
	if len(first.Messages) != 2 || !first.HasMore {
		t.Fatalf("first page: %+v", first)
	}
	next := inbox(t, f.outsider, "?limit=2&before="+first.Messages[1].ID.String())
	if len(next.Messages) != 1 || next.Messages[0].ID != direct.ID || next.HasMore {
		t.Fatalf("second page: %+v", next)
	}

	// Someone who can see the private channel gets that mention.
	if got := inbox(t, f.insider, ""); len(got.Messages) != 1 || got.Messages[0].Content != "@all news" {
		t.Fatalf("staff inbox: %+v", got)
	}
	wantStatus(t, "bad cursor", f.outsider.get("/api/mentions?before=nope"), http.StatusBadRequest)
	wantStatus(t, "anonymous", f.a.anon().get("/api/mentions"), http.StatusUnauthorized)
}
