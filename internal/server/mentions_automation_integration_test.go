//go:build integration

package server

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat/core"
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

	send := func(body map[string]any) core.Message {
		t.Helper()
		res := f.a.exec(h.path+"?wait=true", body, nil)
		wantStatus(t, "execute", res, http.StatusOK)
		var m core.Message
		res.decode(t, &m)
		return m
	}
	notified := func(m core.Message) bool { return len(m.Mentions) == 1 && m.Mentions[0] == member.user.ID }
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
	var m core.Message
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

	send := func(content string, am map[string]any) core.Message {
		t.Helper()
		body := map[string]any{"content": content}
		if am != nil {
			body["allowed_mentions"] = am
		}
		res := f.client.post(path, body)
		wantStatus(t, "send", res, http.StatusCreated)
		var m core.Message
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
	var edited core.Message
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

// upload posts a PNG with a caption through the bot API's upload route;
// allowedMentions is the form field of that name, left out when empty.
func (f *botFixture) upload(caption, allowedMentions string) response {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("content", caption)
	part, _ := mw.CreateFormFile("file", "a.png")
	_, _ = part.Write(pngBytes)
	if allowedMentions != "" {
		_ = mw.WriteField("allowed_mentions", allowedMentions)
	}
	_ = mw.Close()
	req, _ := http.NewRequest(http.MethodPost, f.a.srv.URL+botPath(f.open, "/upload"), &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bot "+f.token.Secret)
	raw, err := http.DefaultClient.Do(req)
	if err != nil {
		f.client.t.Fatal(err)
	}
	defer raw.Body.Close()
	body, _ := io.ReadAll(raw.Body)
	return response{status: raw.StatusCode, body: body}
}

// A bot's upload caption follows the mention rules of a bot's message: @all
// and a role notify only when allowed_mentions asks for them and the bot may.
func TestBotUploadCaptionMentionsFollowAllowedMentions(t *testing.T) {
	f := newBotFixture(t, false)
	f.a.router.Bots.SetLimits(1000, 1000)
	setPerms := func(p perm.Set) {
		t.Helper()
		wantStatus(t, "bot role", f.owner.patch("/api/admin/roles/"+f.roleID.String(), map[string]any{"permissions": p}), http.StatusOK)
	}
	setPerms(perm.SendMessages | perm.AttachFiles | perm.MentionEveryone)
	crew := mentionableRole(t, f.owner, "Oncall", true)
	locked := mentionableRole(t, f.owner, "Locked", false)
	max := f.a.register(f.owner, "max")
	assignRoles(t, f.owner, max, crew, locked)
	f.a.register(f.owner, "bystander")
	send := func(caption, allowedMentions string) core.Message {
		t.Helper()
		res := f.upload(caption, allowedMentions)
		wantStatus(t, "upload", res, http.StatusCreated)
		var m core.Message
		res.decode(t, &m)
		return m
	}
	const everything = `{"parse":["users","everyone","roles"]}`

	// Without the field: members by name only.
	if m := send("@all look, @Oncall too", ""); len(m.Mentions) != 0 || len(m.MentionRoles) != 0 {
		t.Fatalf("notified without allowed_mentions: mentions=%v roles=%v", m.Mentions, m.MentionRoles)
	}
	if m := send("for @max", ""); len(m.Mentions) != 1 || m.Mentions[0] != max.user.ID {
		t.Fatalf("a member named in the caption: mentions=%v", m.Mentions)
	}

	// Asked for, and the bot may mention everyone.
	if m := send("@all look", `{"parse":["everyone"]}`); len(m.Mentions) < 3 || len(m.MentionRoles) != 0 {
		t.Fatalf("@all with parse everyone: mentions=%v roles=%v", m.Mentions, m.MentionRoles)
	}
	if m := send("page @Oncall", `{"parse":["roles"]}`); len(m.Mentions) != 1 || m.Mentions[0] != max.user.ID || len(m.MentionRoles) != 1 || m.MentionRoles[0] != crew {
		t.Fatalf("role with parse roles: mentions=%v roles=%v", m.Mentions, m.MentionRoles)
	}
	if m := send("page @Oncall", `{"roles":["`+crew.String()+`"]}`); len(m.MentionRoles) != 1 {
		t.Fatalf("role listed by ID: roles=%v", m.MentionRoles)
	}
	if m := send("for @max and @all", `{"parse":[]}`); len(m.Mentions) != 0 {
		t.Fatalf("an empty parse notified: mentions=%v", m.Mentions)
	}

	// Asked for, but the bot may not mention everyone: only a mentionable role notifies.
	setPerms(perm.SendMessages | perm.AttachFiles)
	if m := send("@all look, @Locked too", everything); len(m.Mentions) != 0 || len(m.MentionRoles) != 0 {
		t.Fatalf("notified without the permission: mentions=%v roles=%v", m.Mentions, m.MentionRoles)
	}
	if m := send("page @Oncall", everything); len(m.MentionRoles) != 1 || m.MentionRoles[0] != crew {
		t.Fatalf("a mentionable role: roles=%v", m.MentionRoles)
	}

	for _, bad := range []string{`{"parse":`, `[]`, `"everyone"`, `{} {}`} {
		wantStatus(t, "allowed_mentions "+bad, f.upload("x", bad), http.StatusBadRequest)
	}
}
