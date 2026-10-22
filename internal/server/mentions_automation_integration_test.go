//go:build integration

package server

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/perm"
)

func mentionableRole(t *testing.T, owner *client, name string, mentionable bool) uuid.UUID {
	t.Helper()
	id := createRole(t, owner, name, 0)
	wantStatus(t, "mentionable", owner.patch("/api/admin/roles/"+id.String(), map[string]any{"mentionable": mentionable}), http.StatusOK)
	return id
}

func TestWebhookRoleMentionsFollowAllowedMentions(t *testing.T) {
	f := newAccessFixture(t, false)
	ch := f.a.createChannel(f.owner, "alerts", "text")
	crew := mentionableRole(t, f.owner, "Oncall", true)
	locked := mentionableRole(t, f.owner, "Locked", false)
	member := f.a.register(f.owner, "member")
	assignRoles(t, f.owner, member, crew, locked)
	h := makeHook(t, f.owner, ch, "alerts")
	f.a.router.Webhooks.SetLimits(1000, 1000)

	send := func(body map[string]any) chat.Message {
		t.Helper()
		res := f.a.exec(h.path+"?wait=true", body, nil)
		wantStatus(t, "execute", res, http.StatusOK)
		var m chat.Message
		res.decode(t, &m)
		return m
	}
	notified := func(m chat.Message) bool { return len(m.Mentions) == 1 && m.Mentions[0] == member.user.ID }
	am := func(v map[string]any) map[string]any {
		return map[string]any{"content": "page @Oncall now", "allowed_mentions": v}
	}

	// Off by default and when allowed_mentions leaves roles out.
	if m := send(map[string]any{"content": "page @Oncall now"}); len(m.Mentions) != 0 || len(m.MentionRoles) != 0 {
		t.Fatalf("role mentioned without asking: %+v", m)
	}
	if m := send(am(map[string]any{"parse": []string{"users", "everyone"}})); len(m.Mentions) != 0 || len(m.MentionRoles) != 0 {
		t.Fatalf("role mentioned although roles are excluded: %+v", m)
	}
	// parse: roles, or the role listed by ID.
	m := send(am(map[string]any{"parse": []string{"roles"}}))
	if !notified(m) || len(m.MentionRoles) != 1 || m.MentionRoles[0] != crew {
		t.Fatalf("parse roles: %+v", m)
	}
	if m := send(am(map[string]any{"roles": []string{crew.String(), "junk"}})); !notified(m) {
		t.Fatalf("roles list: %+v", m)
	}
	if m := send(am(map[string]any{"roles": []string{locked.String()}})); len(m.Mentions) != 0 {
		t.Fatalf("a role that was not mentioned was listed: %+v", m)
	}
	both := f.a.exec(h.path, am(map[string]any{"parse": []string{"roles"}, "roles": []string{crew.String()}}), nil)
	wantStatus(t, "parse and list", both, http.StatusBadRequest)

	// A role that is not mentionable needs the webhook's own @all setting.
	lockedBody := map[string]any{"content": "@Locked hi", "allowed_mentions": map[string]any{"parse": []string{"roles"}}}
	if m := send(lockedBody); len(m.MentionRoles) != 0 {
		t.Fatalf("locked role without the setting: %+v", m)
	}
	wantStatus(t, "setting", f.owner.patch("/api/admin/webhooks/"+h.ID.String(), map[string]any{"allow_everyone": true}), http.StatusOK)
	if m := send(lockedBody); !notified(m) {
		t.Fatalf("locked role with the setting: %+v", m)
	}
}

func TestWebhookRoleMentionSkipsMembersWhoCannotView(t *testing.T) {
	f := newAccessFixture(t, false)
	secret := f.privateChannel(t, "staff-only", "text")
	crew := mentionableRole(t, f.owner, "Oncall", true)
	assignRoles(t, f.owner, f.outsider, crew)
	assignRoles(t, f.owner, f.insider, f.staff, crew)
	h := makeHook(t, f.owner, secret, "alerts")

	res := f.a.exec(h.path+"?wait=true", map[string]any{"content": "@Oncall", "allowed_mentions": map[string]any{"parse": []string{"roles"}}}, nil)
	wantStatus(t, "execute", res, http.StatusOK)
	var m chat.Message
	res.decode(t, &m)
	if len(m.Mentions) != 1 || m.Mentions[0] != f.insider.user.ID {
		t.Fatalf("only the member who can view the channel is notified: %+v", m.Mentions)
	}
	if rs := f.outsider.readState(t, secret); rs.MentionCount != 0 {
		t.Fatalf("a member who cannot view was notified: %+v", rs)
	}
}

func TestBotRoleMentionsFollowPolicyOnSendAndEdit(t *testing.T) {
	f := newBotFixture(t, false)
	crew := mentionableRole(t, f.owner, "Oncall", true)
	locked := mentionableRole(t, f.owner, "Locked", false)
	member := f.a.register(f.owner, "member")
	assignRoles(t, f.owner, member, crew, locked)
	path := botPath(f.open, "/messages")

	send := func(content string, am map[string]any) chat.Message {
		t.Helper()
		body := map[string]any{"content": content}
		if am != nil {
			body["allowed_mentions"] = am
		}
		res := f.client.post(path, body)
		wantStatus(t, "send", res, http.StatusCreated)
		var m chat.Message
		res.decode(t, &m)
		return m
	}
	roles := map[string]any{"parse": []string{"roles"}}

	if m := send("@Oncall", nil); len(m.MentionRoles) != 0 {
		t.Fatalf("bot mentioned a role without asking: %+v", m)
	}
	if m := send("@Oncall", map[string]any{"parse": []string{"users"}}); len(m.MentionRoles) != 0 {
		t.Fatalf("roles were excluded: %+v", m)
	}
	m := send("@Oncall", roles)
	if len(m.Mentions) != 1 || m.Mentions[0] != member.user.ID || len(m.MentionRoles) != 1 {
		t.Fatalf("bot role mention: %+v", m)
	}
	// An edit sends no allowed_mentions: the role mention goes away.
	res := f.client.patch(path+"/"+m.ID.String(), map[string]any{"content": "@Oncall again"})
	wantStatus(t, "edit", res, http.StatusOK)
	var edited chat.Message
	res.decode(t, &edited)
	if len(edited.MentionRoles) != 0 || len(edited.Mentions) != 0 {
		t.Fatalf("edit kept or added a role mention: %+v", edited)
	}

	// A role that is not mentionable needs the Mention everyone permission.
	if m := send("@Locked", roles); len(m.MentionRoles) != 0 {
		t.Fatalf("locked role without the permission: %+v", m)
	}
	wantStatus(t, "grant", f.owner.patch("/api/admin/roles/"+f.roleID.String(), map[string]any{
		"permissions": perm.SendMessages | perm.AddReactions | perm.AttachFiles | perm.MentionEveryone,
	}), http.StatusOK)
	if m := send("@Locked", roles); len(m.MentionRoles) != 1 || m.MentionRoles[0] != locked {
		t.Fatalf("locked role with the permission: %+v", m)
	}
}
