//go:build integration

package server

import (
	"net/http"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat/core"
)

// mutedMember registers a member and mutes the channel for them.
func mutedMember(t *testing.T, f accessFixture, ch uuid.UUID, name string) *client {
	t.Helper()
	m := f.a.register(f.owner, name)
	wantStatus(t, "mute", m.put("/api/channels/"+ch.String()+"/notifications", map[string]any{"level": "mute"}), http.StatusNoContent)
	return m
}

// KNOWN DEFECT C-L3-N1, pinned as an inverse assertion: it passes while the
// defect exists and fails the day it is fixed (then flip the want to 1).
// doc/architecture.md ("A mute stays silent except for a message addressed to
// the member (by name, through a role they hold, or as a reply)") says a role
// mention breaks a mute, and "@Dev" alone does. "@all @Dev" does not: the
// member is already in the set at kind group through @all, and addRoleMentions
// skips them instead of raising the kind to role.
func TestKnownDefectAllPlusRoleMentionKeepsTheRoleMembersMute(t *testing.T) {
	f := newAccessFixture(t, false)
	ch := f.a.createChannel(f.owner, "general", "text")
	dev := mentionableRole(t, f.owner, "Dev", true)
	alone := mutedMember(t, f, ch, "alone")
	both := mutedMember(t, f, ch, "both")
	assignRoles(t, f.owner, alone, dev)
	assignRoles(t, f.owner, both, dev)

	// Control: "@Dev" alone breaks the mute of a role member.
	postedMessage(t, f.owner, ch, "@Dev standup")
	if rs := alone.readState(t, ch); rs.MentionCount != 1 {
		t.Fatalf("control: @Dev alone must break the mute, got %+v", rs)
	}

	// Defect: "@all @Dev" does not, for a member who holds the role. Read
	// everything first so only this message counts.
	wantStatus(t, "read all", both.post("/api/read-state/read-all", nil), http.StatusNoContent)
	msg := postedMessage(t, f.owner, ch, "@all @Dev standup")
	if len(msg.MentionRoles) != 1 || msg.MentionRoles[0] != dev {
		t.Fatalf("the role is recorded as mentioned: %+v", msg.MentionRoles)
	}
	const wantByDocuments = 1
	if rs := both.readState(t, ch); rs.MentionCount != 0 {
		t.Fatalf("defect fixed? muted role member now counts %d (documents say %d): flip this test", rs.MentionCount, wantByDocuments)
	}
}

// C-L3-N2: a webhook that may not mention members still addresses one by name
// when the message also says @all. Pinned as an inverse assertion.
// doc/webhooks.md: allow_user_mentions off "turns every member mention into
// plain text".
func TestKnownDefectWebhookWithoutUserMentionsStillNamesAMemberAfterAll(t *testing.T) {
	f := newAccessFixture(t, false)
	ch := f.a.createChannel(f.owner, "alerts", "text")
	member := mutedMember(t, f, ch, "member")
	h := makeHook(t, f.owner, ch, "alerts")
	f.a.router.Webhooks.SetLimits(1000, 1000)
	wantStatus(t, "settings", f.owner.patch("/api/admin/webhooks/"+h.ID.String(),
		map[string]any{"allow_user_mentions": false, "allow_everyone": true}), http.StatusOK)

	send := func(content string) core.Message {
		t.Helper()
		body := map[string]any{"content": content, "allowed_mentions": map[string]any{"parse": []string{"everyone"}}}
		res := f.a.exec(h.path+"?wait=true", body, nil)
		wantStatus(t, "execute", res, http.StatusOK)
		var m core.Message
		res.decode(t, &m)
		return m
	}
	if m := send("@member alone"); len(m.Mentions) != 0 {
		t.Fatalf("control: user mentions off, @member must be plain text: %+v", m.Mentions)
	}
	if rs := member.readState(t, ch); rs.MentionCount != 0 {
		t.Fatalf("control: %+v", rs)
	}
	// @all lists everybody; what differs is the kind stored for the member,
	// which the muted count shows: the name wins over the group.
	if m := send("@all @member"); !slices.Contains(m.Mentions, member.user.ID) {
		t.Fatalf("@all lists every member: %+v", m.Mentions)
	}
	if rs := member.readState(t, ch); rs.MentionCount != 1 {
		t.Fatalf("defect fixed? the muted member's count is %d, was 1 through @all plus the name", rs.MentionCount)
	}
}
