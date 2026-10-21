//go:build integration

package server

import (
	"net/http"
	"slices"
	"testing"
)

// A bot's edit reaches no more people than its sends do: role mentions stay
// plain text for a bot that may not mention everyone.
func TestBotEditDoesNotReachRoleMentions(t *testing.T) {
	f := newBotFixture(t, false)
	crew := createRole(t, f.owner, "Crew", 0)
	wantStatus(t, "mentionable", f.owner.patch("/api/admin/roles/"+crew.String(), map[string]any{"mentionable": true}), http.StatusOK)
	member := f.a.register(f.owner, "member")
	assignRoles(t, f.owner, member, crew)

	sent := f.client.say(f.open, "@Crew assemble")
	wantStatus(t, "bot send", sent, http.StatusCreated)
	var msg inboxMessage
	sent.decode(t, &msg)
	if len(msg.MentionRoles) != 0 || member.readState(t, f.open).MentionCount != 0 {
		t.Fatalf("a bot send notified a role: %+v", msg)
	}

	hello := f.client.say(f.open, "hello")
	wantStatus(t, "bot send", hello, http.StatusCreated)
	hello.decode(t, &msg)
	edited := f.client.patch(botPath(f.open, "/messages/"+msg.ID.String()), map[string]any{"content": "@Crew assemble"})
	wantStatus(t, "bot edit", edited, http.StatusOK)
	edited.decode(t, &msg)
	if len(msg.MentionRoles) != 0 || member.readState(t, f.open).MentionCount != 0 {
		t.Fatalf("a bot edit notified a role: %+v", msg)
	}
}

// A multi-word role name is one mention: the user named like its first word
// is not also notified, while a single-word role name still yields to a user.
func TestPhraseRoleMentionDoesNotNotifyTheFirstWordsUser(t *testing.T) {
	f := newAccessFixture(t, false)
	ch := f.a.createChannel(f.owner, "general", "text")
	team := createRole(t, f.owner, "Dev Team", 0)
	wantStatus(t, "mentionable", f.owner.patch("/api/admin/roles/"+team.String(), map[string]any{"mentionable": true}), http.StatusOK)
	member := f.a.register(f.owner, "member")
	assignRoles(t, f.owner, member, team)
	dev := f.a.register(f.owner, "dev")

	postedMessage(t, f.owner, ch, "@Dev Team standup")
	if rs := member.readState(t, ch); rs.MentionCount != 1 {
		t.Fatalf("the role member was not notified: %+v", rs)
	}
	if rs := dev.readState(t, ch); rs.MentionCount != 0 {
		t.Fatalf("the user named dev was notified by @Dev Team: %+v", rs)
	}

	// Written on its own, @dev is still the user.
	postedMessage(t, f.owner, ch, "@dev and @Dev Team")
	if rs := dev.readState(t, ch); rs.MentionCount != 1 {
		t.Fatalf("@dev no longer reaches the user: %+v", rs)
	}
}

// A reply that also mentions its parent's author appears once in the inbox,
// and paging across the two sources keeps one ordered list.
func TestMentionsInboxListsAReplyThatMentionsOnce(t *testing.T) {
	f := newAccessFixture(t, false)
	ch := f.a.createChannel(f.owner, "general", "text")
	mine := postedMessage(t, f.outsider, ch, "my own words")
	both := f.owner.post("/api/channels/"+ch.String()+"/messages", map[string]any{"content": "@outsider agreed", "reply_to_id": mine.ID})
	wantStatus(t, "reply with mention", both, http.StatusCreated)
	var replied inboxMessage
	both.decode(t, &replied)
	direct := postedMessage(t, f.owner, ch, "psst @outsider")

	page := inbox(t, f.outsider, "")
	var got []string
	for _, m := range page.Messages {
		got = append(got, m.ID.String())
	}
	if want := []string{direct.ID.String(), replied.ID.String()}; !slices.Equal(got, want) || page.HasMore {
		t.Fatalf("inbox = %v (more=%v), want %v", got, page.HasMore, want)
	}
	first := inbox(t, f.outsider, "?limit=1")
	next := inbox(t, f.outsider, "?limit=1&before="+first.Messages[0].ID.String())
	if !first.HasMore || len(next.Messages) != 1 || next.Messages[0].ID != replied.ID || next.HasMore {
		t.Fatalf("paging: %+v then %+v", first, next)
	}
}
