//go:build integration

package server

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/perm"
)

type accessFixture struct {
	a        *app
	owner    *client
	insider  *client // holds the Staff role
	outsider *client // a plain member
	staff    uuid.UUID
	everyone uuid.UUID
}

func newAccessFixture(t *testing.T, withHub bool) accessFixture {
	t.Helper()
	a := newApp(t, withHub)
	owner := a.seedAdmin()
	f := accessFixture{a: a, owner: owner, insider: a.register(owner, "insider"), outsider: a.register(owner, "outsider")}
	f.staff = createRole(t, owner, "Staff", 0)
	f.everyone = everyoneID(t, owner)
	assignRoles(t, owner, f.insider, f.staff)
	return f
}

// privateChannel hides a channel from @everyone and shows it to Staff.
func (f accessFixture) privateChannel(t *testing.T, name, kind string) uuid.UUID {
	t.Helper()
	ch := f.a.createChannel(f.owner, name, kind)
	wantStatus(t, "make private", putOverwrites(f.owner, "channels", ch,
		overwrite("role", f.everyone, 0, perm.ViewChannel),
		overwrite("role", f.staff, perm.ViewChannel, 0)), http.StatusNoContent)
	return ch
}

func TestPrivateChannelIsInvisibleEverywhereToOutsiders(t *testing.T) {
	f := newAccessFixture(t, false)
	secret := f.privateChannel(t, "staff-only", "text")
	open := f.a.createChannel(f.owner, "open", "text")
	base := "/api/channels/" + secret.String()

	first := f.owner.post(base+"/messages", map[string]any{"content": "the plan is a secret, ping @outsider @insider"})
	wantStatus(t, "first message", first, http.StatusCreated)
	var created struct {
		ID     uuid.UUID
		Number int64
	}
	first.decode(t, &created)
	msg := created.ID
	root := postMessage(t, f.owner, secret, map[string]any{"content": "thread root"})
	postMessage(t, f.owner, secret, map[string]any{"content": "in thread", "parent_id": root})
	up := f.owner.upload(base+"/upload", "file", "plan.png", "image/png", pngBytes, nil)
	wantStatus(t, "owner upload", up, http.StatusCreated)
	var shot uploaded
	up.decode(t, &shot)
	number := created

	// Outsiders: the channel does not exist.
	out := f.outsider
	if got := visibleChannelIDs(t, out); got[secret] || !got[open] {
		t.Fatalf("outsider's channels: %v", got)
	}
	if info := myPerms(t, out); channelPerms(info, secret) != 0 || channelPerms(info, open) == 0 {
		t.Fatalf("outsider's channel permissions: %v", info.Channels)
	}
	for _, c := range []struct {
		what, method, path string
		body               any
	}{
		{"history", "GET", base + "/messages", nil},
		{"post", "POST", base + "/messages", map[string]any{"content": "hi"}},
		{"upload", "POST", base + "/upload", nil},
		{"edit", "PUT", base + "/messages/" + msg.String(), map[string]string{"content": "x"}},
		{"delete", "DELETE", base + "/messages/" + msg.String(), nil},
		{"react", "POST", "/api/messages/" + msg.String() + "/reactions", map[string]string{"emoji": "👍"}},
		{"thread", "GET", "/api/messages/" + root.String() + "/thread", nil},
		{"mark read", "POST", base + "/read", nil},
		{"mark unread", "POST", base + "/unread", map[string]string{"message_id": msg.String()}},
		{"notifications", "PUT", base + "/notifications", map[string]string{"level": "mute"}},
		{"attachment", "GET", shot.Attachments[0].URL, nil},
		{"by number", "GET", "/api/messages/by-number/" + strconv.FormatInt(number.Number, 10), nil},
		{"search by channel", "GET", "/api/search?channel_id=" + secret.String(), nil},
		{"overwrites", "GET", "/api/admin/channels/" + secret.String() + "/overwrites", nil},
	} {
		res := out.do(c.method, c.path, c.body, nil)
		want := http.StatusNotFound
		if c.what == "overwrites" {
			want = http.StatusForbidden // no admin rights at all
		}
		if res.status != want {
			t.Errorf("%s as an outsider: %d %s, want %d", c.what, res.status, res.body, want)
		}
	}
	var search struct{ Messages []struct{ Content string } }
	out.get("/api/search?q=secret").decode(t, &search)
	if len(search.Messages) != 0 {
		t.Fatalf("search leaks a private channel: %+v", search)
	}
	if rs := out.readState(t, secret); rs.UnreadCount != 0 || rs.MentionCount != 0 {
		t.Fatalf("read state leaks: %+v", rs)
	}
	var all []readState
	out.get("/api/read-state").decode(t, &all)
	for _, s := range all {
		if s.ChannelID == secret {
			t.Fatal("read-state lists the private channel")
		}
	}

	// Insiders work normally, including the mention that reached them.
	in := f.insider
	if got := visibleChannelIDs(t, in); !got[secret] {
		t.Fatalf("insider's channels: %v", got)
	}
	wantStatus(t, "insider history", in.get(base+"/messages"), http.StatusOK)
	wantStatus(t, "insider post", in.post(base+"/messages", map[string]any{"content": "got it"}), http.StatusCreated)
	wantStatus(t, "insider attachment", in.get(shot.Attachments[0].URL), http.StatusOK)
	wantStatus(t, "insider thread", in.get("/api/messages/"+root.String()+"/thread"), http.StatusOK)
	wantStatus(t, "insider by number", in.get("/api/messages/by-number/"+strconv.FormatInt(number.Number, 10)), http.StatusOK)
	in.get("/api/search?q=secret").decode(t, &search)
	if len(search.Messages) != 1 {
		t.Fatalf("insider search: %+v", search)
	}
	if rs := in.readState(t, secret); rs.MentionCount != 1 {
		t.Fatalf("the insider's mention: %+v", rs)
	}
	// Nobody who cannot see a channel is notified about a mention in it.
	if rs := out.readState(t, secret); rs.MentionCount != 0 {
		t.Fatalf("mention reached an outsider: %+v", rs)
	}
	// The Owner sees everything without being listed anywhere.
	wantStatus(t, "owner history", f.owner.get(base+"/messages"), http.StatusOK)
}

func TestChannelPermissionsGateTheirActions(t *testing.T) {
	f := newAccessFixture(t, false)
	ch := f.a.createChannel(f.owner, "news", "text")
	base := "/api/channels/" + ch.String()
	msg := postMessage(t, f.owner, ch, map[string]any{"content": "official"})
	own := postMessage(t, f.outsider, ch, map[string]any{"content": "mine"})

	// Read-only for @everyone, writing for Staff.
	wantStatus(t, "lock", putOverwrites(f.owner, "channels", ch,
		overwrite("role", f.everyone, 0, perm.SendMessages|perm.AttachFiles|perm.AddReactions),
		overwrite("role", f.staff, perm.SendMessages, 0)), http.StatusNoContent)

	out := f.outsider
	wantStatus(t, "read still works", out.get(base+"/messages"), http.StatusOK)
	for _, c := range []struct {
		what, method, path string
		body               any
	}{
		{"post", "POST", base + "/messages", map[string]any{"content": "hi"}},
		{"edit own", "PUT", base + "/messages/" + own.String(), map[string]string{"content": "x"}},
		{"react", "POST", "/api/messages/" + msg.String() + "/reactions", map[string]string{"emoji": "👍"}},
	} {
		if res := out.do(c.method, c.path, c.body, nil); res.status != http.StatusForbidden {
			t.Errorf("%s without permission: %d %s", c.what, res.status, res.body)
		}
	}
	// Taking back an own reaction (given earlier) stays possible.
	wantStatus(t, "delete own message", out.delete(base+"/messages/"+own.String()), http.StatusNoContent)

	in := f.insider
	wantStatus(t, "staff posts", in.post(base+"/messages", map[string]any{"content": "news!"}), http.StatusCreated)
	// Staff may write but not attach (the @everyone deny still applies) or react.
	wantStatus(t, "staff upload", in.upload(base+"/upload", "file", "a.png", "image/png", pngBytes, nil), http.StatusForbidden)
	wantStatus(t, "staff reacts", in.post("/api/messages/"+msg.String()+"/reactions", map[string]string{"emoji": "👍"}), http.StatusForbidden)

	// A reaction made before the permission went can be taken back.
	other := f.a.createChannel(f.owner, "talk", "text")
	m2 := postMessage(t, f.owner, other, map[string]any{"content": "x"})
	wantStatus(t, "react", out.post("/api/messages/"+m2.String()+"/reactions", map[string]string{"emoji": "🎉"}), http.StatusOK)
	wantStatus(t, "deny reactions", putOverwrites(f.owner, "channels", other, overwrite("role", f.everyone, 0, perm.AddReactions)), http.StatusNoContent)
	wantStatus(t, "add another", out.post("/api/messages/"+m2.String()+"/reactions", map[string]string{"emoji": "🔥"}), http.StatusForbidden)
	wantStatus(t, "take back", out.post("/api/messages/"+m2.String()+"/reactions", map[string]string{"emoji": "🎉"}), http.StatusOK)

	// Attach files alone gates uploads.
	wantStatus(t, "deny attach", putOverwrites(f.owner, "channels", other, overwrite("role", f.everyone, 0, perm.AttachFiles)), http.StatusNoContent)
	wantStatus(t, "no attach", out.upload("/api/channels/"+other.String()+"/upload", "file", "a.png", "image/png", pngBytes, nil), http.StatusForbidden)
	wantStatus(t, "plain message", out.post("/api/channels/"+other.String()+"/messages", map[string]any{"content": "still ok"}), http.StatusCreated)
}

func TestManageMessagesIsPerChannel(t *testing.T) {
	f := newAccessFixture(t, false)
	moderated := f.a.createChannel(f.owner, "moderated", "text")
	elsewhere := f.a.createChannel(f.owner, "elsewhere", "text")
	a := postMessage(t, f.outsider, moderated, map[string]any{"content": "spam"})
	b := postMessage(t, f.outsider, elsewhere, map[string]any{"content": "spam"})
	// Staff may delete messages in "moderated" only.
	wantStatus(t, "grant", putOverwrites(f.owner, "channels", moderated, overwrite("role", f.staff, perm.ManageMessages, 0)), http.StatusNoContent)
	if p := myPerms(t, f.insider); !channelPerms(p, moderated).Has(perm.ManageMessages) || channelPerms(p, elsewhere).Has(perm.ManageMessages) {
		t.Fatalf("channel scoped permission: %+v", p.Channels)
	}
	wantStatus(t, "elsewhere", f.insider.delete("/api/channels/"+elsewhere.String()+"/messages/"+b.String()), http.StatusForbidden)
	wantStatus(t, "moderated", f.insider.delete("/api/channels/"+moderated.String()+"/messages/"+a.String()), http.StatusNoContent)
	// Granted server-wide through the role itself, it works everywhere.
	wantStatus(t, "role grant", f.owner.patch("/api/admin/roles/"+f.staff.String(), map[string]any{"permissions": perm.ManageMessages}), http.StatusOK)
	wantStatus(t, "elsewhere now", f.insider.delete("/api/channels/"+elsewhere.String()+"/messages/"+b.String()), http.StatusNoContent)
	// Wrong channel in the path is still a 404.
	c := postMessage(t, f.outsider, moderated, map[string]any{"content": "x"})
	wantStatus(t, "mixed up path", f.insider.delete("/api/channels/"+elsewhere.String()+"/messages/"+c.String()), http.StatusNotFound)
}

func TestMentionPolicy(t *testing.T) {
	f := newAccessFixture(t, false)
	ch := f.a.createChannel(f.owner, "general", "text")
	// @everyone loses the right to mention everyone; Staff keeps it.
	wantStatus(t, "deny", putOverwrites(f.owner, "channels", ch,
		overwrite("role", f.everyone, 0, perm.MentionEveryone),
		overwrite("role", f.staff, perm.MentionEveryone, 0)), http.StatusNoContent)
	other := f.a.register(f.owner, "other")

	postMessage(t, other, ch, map[string]any{"content": "@all wake up"})
	if rs := f.insider.readState(t, ch); rs.MentionCount != 0 {
		t.Fatalf("@all without the permission notified: %+v", rs)
	}
	postMessage(t, other, ch, map[string]any{"content": "psst @insider"})
	if rs := f.insider.readState(t, ch); rs.MentionCount != 1 {
		t.Fatalf("a direct mention still works: %+v", rs)
	}
	postMessage(t, f.insider, ch, map[string]any{"content": "@all meeting"})
	if rs := other.readState(t, ch); rs.MentionCount != 1 {
		t.Fatalf("@all with the permission: %+v", rs)
	}
	// Editing cannot smuggle it in.
	id := postMessage(t, other, ch, map[string]any{"content": "calm"})
	wantStatus(t, "edit", other.put("/api/channels/"+ch.String()+"/messages/"+id.String(), map[string]string{"content": "@all now"}), http.StatusOK)
	if rs := f.insider.readState(t, ch); rs.MentionCount != 1 {
		t.Fatalf("an edit smuggled @all in: %+v", rs)
	}
}

func TestCategoryOverwritesInheritAndChannelsWin(t *testing.T) {
	f := newAccessFixture(t, false)
	catRes := f.owner.post("/api/admin/categories", map[string]any{"name": "Staff area"})
	wantStatus(t, "category", catRes, http.StatusCreated)
	var cat struct{ ID uuid.UUID }
	catRes.decode(t, &cat)
	chRes := f.owner.post("/api/admin/channels", map[string]any{"name": "inside", "category_id": cat.ID})
	wantStatus(t, "channel", chRes, http.StatusCreated)
	var inside struct{ ID uuid.UUID }
	chRes.decode(t, &inside)
	wantStatus(t, "sibling", f.owner.post("/api/admin/channels", map[string]any{"name": "also inside", "category_id": cat.ID}), http.StatusCreated)

	wantStatus(t, "hide category", putOverwrites(f.owner, "categories", cat.ID,
		overwrite("role", f.everyone, 0, perm.ViewChannel),
		overwrite("role", f.staff, perm.ViewChannel, 0)), http.StatusNoContent)
	if categoryIDs(t, f.outsider)[cat.ID] || len(visibleChannelIDs(t, f.outsider)) != 0 {
		t.Fatal("the category and its channels must be hidden from outsiders")
	}
	if !categoryIDs(t, f.insider)[cat.ID] || len(visibleChannelIDs(t, f.insider)) != 2 {
		t.Fatal("insiders see the category")
	}
	wantStatus(t, "inherited", f.outsider.get("/api/channels/"+inside.ID.String()+"/messages"), http.StatusNotFound)

	// The channel's own overwrite wins over its category's.
	wantStatus(t, "reopen one", putOverwrites(f.owner, "channels", inside.ID, overwrite("role", f.everyone, perm.ViewChannel, 0)), http.StatusNoContent)
	wantStatus(t, "reopened", f.outsider.get("/api/channels/"+inside.ID.String()+"/messages"), http.StatusOK)
	if got := visibleChannelIDs(t, f.outsider); len(got) != 1 || !got[inside.ID] {
		t.Fatalf("outsider sees %v", got)
	}
	if !categoryIDs(t, f.outsider)[cat.ID] {
		t.Fatal("a category with a visible channel shows")
	}

	// Reading back what was set.
	var back OverwritesResponse
	f.owner.get("/api/admin/categories/"+cat.ID.String()+"/overwrites").decode(t, &back)
	if len(back.Overwrites) != 2 {
		t.Fatalf("overwrites: %+v", back)
	}
	wantStatus(t, "unknown channel", f.owner.get("/api/admin/channels/"+uuid.New().String()+"/overwrites"), http.StatusNotFound)
	wantStatus(t, "unknown category", f.owner.get("/api/admin/categories/"+uuid.New().String()+"/overwrites"), http.StatusNotFound)

	// Deleting a private category must not make its channels public.
	wantStatus(t, "un-reopen", putOverwrites(f.owner, "channels", inside.ID), http.StatusNoContent)
	wantStatus(t, "delete category", f.owner.delete("/api/admin/categories/"+cat.ID.String()), http.StatusNoContent)
	if got := visibleChannelIDs(t, f.outsider); len(got) != 0 {
		t.Fatalf("deleting the category exposed channels: %v", got)
	}
	if got := visibleChannelIDs(t, f.insider); len(got) != 2 {
		t.Fatalf("insiders lost the channels: %v", got)
	}
	// ...and a channel's own entries still win over what was folded in.
	wantStatus(t, "own entry wins", putOverwrites(f.owner, "channels", inside.ID, overwrite("role", f.everyone, perm.ViewChannel, 0)), http.StatusNoContent)
	if got := visibleChannelIDs(t, f.outsider); !got[inside.ID] {
		t.Fatalf("channel overwrite: %v", got)
	}
	wantStatus(t, "unknown category delete", f.owner.delete("/api/admin/categories/"+cat.ID.String()), http.StatusNotFound)
}

// OverwritesResponse mirrors the API body for tests.
type OverwritesResponse struct {
	Overwrites []struct {
		Type  string
		ID    uuid.UUID
		Allow perm.Set
		Deny  perm.Set
	}
}

func TestOverwriteValidationAndLimits(t *testing.T) {
	f := newAccessFixture(t, false)
	ch := f.a.createChannel(f.owner, "x", "text")
	mod := f.a.register(f.owner, "mod")
	role := createRole(t, f.owner, "Channel admin", perm.ManageChannels|perm.ViewChannel|perm.SendMessages)
	assignRoles(t, f.owner, mod, role)
	order(t, f.owner, role, f.staff) // the editor outranks the roles they edit

	for _, c := range []struct {
		what   string
		list   []map[string]any
		status int
	}{
		{"server-wide bit", []map[string]any{overwrite("role", f.staff, perm.ManageRoles, 0)}, http.StatusBadRequest},
		{"unknown bit", []map[string]any{overwrite("role", f.staff, 1<<40, 0)}, http.StatusBadRequest},
		{"allow and deny", []map[string]any{overwrite("role", f.staff, perm.Speak, perm.Speak)}, http.StatusBadRequest},
		{"duplicate", []map[string]any{overwrite("role", f.staff, perm.Speak, 0), overwrite("role", f.staff, 0, perm.Speak)}, http.StatusBadRequest},
		{"unknown role", []map[string]any{overwrite("role", uuid.New(), perm.Speak, 0)}, http.StatusBadRequest},
		{"unknown member", []map[string]any{overwrite("member", uuid.New(), perm.Speak, 0)}, http.StatusBadRequest},
		{"unknown type", []map[string]any{overwrite("group", f.staff, perm.Speak, 0)}, http.StatusBadRequest},
		{"allow what you lack", []map[string]any{overwrite("role", f.staff, perm.Connect|perm.ManageMessages, 0)}, http.StatusForbidden},
		{"deny is always fine", []map[string]any{overwrite("role", f.staff, 0, perm.Connect)}, http.StatusNoContent},
		{"allow what you hold", []map[string]any{overwrite("role", f.staff, perm.SendMessages, 0)}, http.StatusNoContent},
		{"for a member", []map[string]any{overwrite("member", f.outsider.user.ID, 0, perm.ViewChannel)}, http.StatusNoContent},
		{"empty clears", nil, http.StatusNoContent},
	} {
		wantStatus(t, c.what, putOverwrites(mod, "channels", ch, c.list...), c.status)
	}
	wantStatus(t, "unknown target", putOverwrites(mod, "channels", uuid.New()), http.StatusNotFound)
	wantStatus(t, "unknown category", putOverwrites(mod, "categories", uuid.New()), http.StatusNotFound)
	wantStatus(t, "members have no say", putOverwrites(f.outsider, "channels", ch), http.StatusForbidden)
	// A member overwrite hides the channel from just that member.
	wantStatus(t, "hide from one member", putOverwrites(f.owner, "channels", ch, overwrite("member", f.outsider.user.ID, 0, perm.ViewChannel)), http.StatusNoContent)
	if visibleChannelIDs(t, f.outsider)[ch] || !visibleChannelIDs(t, f.insider)[ch] {
		t.Fatal("a member overwrite applies to that member only")
	}
	// A manager who cannot see a channel cannot manage it blindly.
	wantStatus(t, "hide from the manager", putOverwrites(f.owner, "channels", ch, overwrite("member", mod.user.ID, 0, perm.ViewChannel)), http.StatusNoContent)
	wantStatus(t, "blind edit", putOverwrites(mod, "channels", ch), http.StatusNotFound)
	wantStatus(t, "blind read", mod.get("/api/admin/channels/"+ch.String()+"/overwrites"), http.StatusNotFound)
	wantStatus(t, "blind rename", mod.patch("/api/admin/channels/"+ch.String(), map[string]any{"name": "y"}), http.StatusNotFound)
	wantStatus(t, "blind delete", mod.delete("/api/admin/channels/"+ch.String()), http.StatusNotFound)
	wantStatus(t, "blind duplicate", mod.post("/api/admin/channels/"+ch.String()+"/duplicate", nil), http.StatusNotFound)
	wantStatus(t, "blind layout", mod.put("/api/admin/layout", map[string]any{"channels": []map[string]any{{"id": ch, "category_id": nil, "sort_order": 3}}}), http.StatusNotFound)
	wantStatus(t, "manager still creates", mod.post("/api/admin/channels", map[string]any{"name": "new"}), http.StatusCreated)
	// ...nor a category that is hidden from them.
	catRes := f.owner.post("/api/admin/categories", map[string]any{"name": "Hidden"})
	wantStatus(t, "category", catRes, http.StatusCreated)
	var cat struct{ ID uuid.UUID }
	catRes.decode(t, &cat)
	wantStatus(t, "hide the category from the manager", putOverwrites(f.owner, "categories", cat.ID, overwrite("member", mod.user.ID, 0, perm.ViewChannel)), http.StatusNoContent)
	wantStatus(t, "blind category read", mod.get("/api/admin/categories/"+cat.ID.String()+"/overwrites"), http.StatusNotFound)
	wantStatus(t, "blind category rename", mod.patch("/api/admin/categories/"+cat.ID.String(), map[string]any{"name": "x"}), http.StatusNotFound)
}

func TestDuplicatedChannelsKeepTheirPermissions(t *testing.T) {
	f := newAccessFixture(t, false)
	secret := f.privateChannel(t, "staff-only", "text")
	res := f.owner.post("/api/admin/channels/"+secret.String()+"/duplicate", nil)
	wantStatus(t, "duplicate", res, http.StatusCreated)
	var copy struct{ ID uuid.UUID }
	res.decode(t, &copy)
	if visibleChannelIDs(t, f.outsider)[copy.ID] || !visibleChannelIDs(t, f.insider)[copy.ID] {
		t.Fatal("the copy of a private channel must stay private")
	}
	var back OverwritesResponse
	f.owner.get("/api/admin/channels/"+copy.ID.String()+"/overwrites").decode(t, &back)
	if len(back.Overwrites) != 2 {
		t.Fatalf("copied overwrites: %+v", back)
	}
}

func TestManageChannelsSplitsFromOtherAdminPowers(t *testing.T) {
	f := newAccessFixture(t, false)
	builder := f.a.register(f.owner, "builder")
	role := createRole(t, f.owner, "Builder", perm.ManageChannels)
	assignRoles(t, f.owner, builder, role)
	wantStatus(t, "create category", builder.post("/api/admin/categories", map[string]any{"name": "New"}), http.StatusCreated)
	wantStatus(t, "create channel", builder.post("/api/admin/channels", map[string]any{"name": "fresh"}), http.StatusCreated)
	wantStatus(t, "no invites", builder.get("/api/admin/invites"), http.StatusForbidden)
	wantStatus(t, "no accounts", builder.get("/api/admin/users"), http.StatusForbidden)
	wantStatus(t, "no roles", builder.post("/api/admin/roles", map[string]any{"name": "x"}), http.StatusForbidden)
	wantStatus(t, "no media", builder.get("/api/admin/media"), http.StatusForbidden)
	wantStatus(t, "no system", builder.get("/api/admin/system"), http.StatusForbidden)
}
