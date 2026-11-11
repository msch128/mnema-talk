//go:build integration

package server

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/perm"
)

// TestEveryChannelScopedRouteHidesAPrivateChannelFromOutsiders probes every
// route that takes a channel, message, post, tag, poll or attachment id with
// real ids of a channel the caller cannot see. The answer must be the same
// "not found" as for an id that does not exist, never a validation error or a
// success, so the response tells an outsider nothing about the channel.
func TestEveryChannelScopedRouteHidesAPrivateChannelFromOutsiders(t *testing.T) {
	f := newAccessFixture(t, false)
	text := f.privateChannel(t, "staff-text", "text")
	forum := f.privateChannel(t, "staff-forum", "forum")
	tb := "/api/channels/" + text.String()
	fb := "/api/channels/" + forum.String()

	msg := postMessage(t, f.owner, text, map[string]any{"content": "root"})
	wantStatus(t, "pin", f.owner.put(tb+"/messages/"+msg.String()+"/pin", nil), http.StatusOK)
	var poll struct{ ID uuid.UUID }
	res := f.owner.post(tb+"/polls", map[string]any{"question": "Q?", "options": []string{"a", "b"}})
	wantStatus(t, "poll", res, http.StatusCreated)
	res.decode(t, &poll)
	up := f.owner.upload(tb+"/attachments", "file", "a.png", "image/png", pngBytes, nil)
	wantStatus(t, "attachment", up, http.StatusCreated)
	var att struct{ ID uuid.UUID }
	up.decode(t, &att)
	var post struct{ ID uuid.UUID }
	res = f.owner.post(fb+"/posts", map[string]any{"title": "post", "content": "body"})
	wantStatus(t, "post", res, http.StatusCreated)
	res.decode(t, &post)
	var tag struct{ ID uuid.UUID }
	res = f.owner.post(fb+"/tags", map[string]any{"name": "t"})
	wantStatus(t, "tag", res, http.StatusCreated)
	res.decode(t, &tag)
	m := msg.String()

	probes := []channelProbe{
		{"messages", "GET", tb + "/messages", nil},
		{"send", "POST", tb + "/messages", map[string]any{"content": "x"}},
		{"edit message", "PUT", tb + "/messages/" + m, map[string]any{"content": "x"}},
		{"delete message", "DELETE", tb + "/messages/" + m, nil},
		{"mark read", "POST", tb + "/read", map[string]any{"message_id": msg}},
		{"mark unread", "POST", tb + "/unread", map[string]any{"message_id": msg}},
		{"upload", "POST", tb + "/upload", nil},
		{"notifications", "PUT", tb + "/notifications", map[string]any{"level": "mentions"}},
		{"commands", "GET", tb + "/commands", nil},
		{"first unread", "GET", tb + "/first-unread", nil},
		{"edit history", "GET", tb + "/messages/" + m + "/edits", nil},
		{"poll", "GET", tb + "/messages/" + poll.ID.String() + "/poll", nil},
		{"poll vote", "PUT", tb + "/messages/" + poll.ID.String() + "/poll/votes", map[string]any{"options": []int{0}}},
		{"poll end", "POST", tb + "/messages/" + poll.ID.String() + "/poll/end", nil},
		{"pins", "GET", tb + "/pins", nil},
		{"pin", "PUT", tb + "/messages/" + m + "/pin", nil},
		{"unpin", "DELETE", tb + "/messages/" + m + "/pin", nil},
		{"new poll", "POST", tb + "/polls", map[string]any{"question": "Q?", "options": []string{"a", "b"}}},
		{"scheduled", "POST", tb + "/scheduled", map[string]any{"content": "x", "send_at": time.Now().Add(time.Hour)}},
		{"attachment upload", "POST", tb + "/attachments", nil},
		{"attachment delete", "DELETE", tb + "/attachments/" + att.ID.String(), nil},
		{"bulk delete", "POST", tb + "/messages/bulk-delete", map[string]any{"user_id": f.owner.user.ID, "count": 1}},
		{"forward from", "GET", "/api/messages/" + m + "/forward", nil},
		{"report", "POST", "/api/messages/" + m + "/report", map[string]any{"category": "spam"}},
		{"save", "PUT", "/api/me/saved/" + m, nil},
		{"remind", "POST", "/api/me/reminders", map[string]any{"message_id": m, "remind_at": time.Now().Add(time.Hour)}},
		{"follow", "PUT", "/api/threads/" + m + "/follow", nil},
		{"unfollow", "DELETE", "/api/threads/" + m + "/follow", nil},
		{"interactions", "POST", tb + "/interactions", map[string]any{"command_id": uuid.New()}},
		{"forum settings", "GET", fb + "/forum-settings", nil},
		{"forum settings edit", "PATCH", fb + "/forum-settings", map[string]any{}},
		{"posts", "GET", fb + "/posts", nil},
		{"posts unread", "GET", fb + "/posts/unread", nil},
		{"new post", "POST", fb + "/posts", map[string]any{"title": "x", "content": "y"}},
		{"edit post", "PATCH", fb + "/posts/" + post.ID.String(), map[string]any{"title": "x"}},
		{"read post", "PUT", fb + "/posts/" + post.ID.String() + "/read", nil},
		{"tags", "GET", fb + "/tags", nil},
		{"new tag", "POST", fb + "/tags", map[string]any{"name": "x"}},
		{"edit tag", "PATCH", fb + "/tags/" + tag.ID.String(), map[string]any{"name": "x"}},
		{"delete tag", "DELETE", fb + "/tags/" + tag.ID.String(), nil},
		{"order tags", "PUT", fb + "/tags/order", map[string]any{"tag_ids": []uuid.UUID{tag.ID}}},
		{"voice disconnect", "POST", "/api/channels/" + text.String() + "/voice/" + f.insider.user.ID.String() + "/disconnect", nil},
	}
	// A member who manages channels and roles, but is not let into these two.
	mod := f.a.register(f.owner, "moderator")
	assignRoles(t, f.owner, mod, createRole(t, f.owner, "Channel mods", perm.ManageChannels|perm.ManageRoles))
	ab := "/api/admin/channels/" + text.String()
	adminProbes := []channelProbe{
		{"edit channel", "PATCH", ab, map[string]any{"name": "renamed"}},
		{"delete channel", "DELETE", ab, nil},
		{"duplicate channel", "POST", ab + "/duplicate", map[string]any{}},
		{"auto-delete", "PUT", ab + "/auto-delete", map[string]any{"days": 7}},
		{"overwrites", "GET", ab + "/overwrites", nil},
		{"set overwrites", "PUT", ab + "/overwrites", map[string]any{"overwrites": []any{}}},
	}

	hidden := []string{text.String(), forum.String()}
	for _, p := range probes {
		p.mustLookNonexistent(t, f.outsider, hidden)
	}
	for _, p := range adminProbes {
		p.mustLookNonexistent(t, mod, hidden)
	}
	requireEveryChannelRouteProbed(t, f.a, append(probes, adminProbes...))
}

// channelProbe is one request that names a channel the caller cannot see.
type channelProbe struct {
	what, method, path string
	body               any
}

func (p channelProbe) send(c *client, path string) response {
	if p.body == nil && p.method == http.MethodPost && (strings.HasSuffix(path, "/attachments") || strings.HasSuffix(path, "/upload")) {
		return c.upload(path, "file", "a.png", "image/png", pngBytes, nil)
	}
	return c.do(p.method, path, p.body, nil)
}

// mustLookNonexistent sends the probe as c and requires a 404. A route with the
// channel in its path must answer in the same bytes as the same request with a
// channel id nobody has (perm.ErrChannelHidden, or the 404 of what the route
// looks up in the channel), so the answer does not say the channel exists.
func (p channelProbe) mustLookNonexistent(t *testing.T, c *client, hidden []string) {
	t.Helper()
	res := p.send(c, p.path)
	if p.what == "voice disconnect" {
		// The hierarchy check on the target comes first: still a refusal that
		// says nothing about the channel.
		if res.status != http.StatusForbidden {
			t.Errorf("%s %s: %d %s, want 403", p.method, p.what, res.status, res.body)
		}
		return
	}
	if res.status != http.StatusNotFound {
		t.Errorf("%s %s: %d %s, want 404", p.method, p.what, res.status, res.body)
		return
	}
	unknown := p.path
	for _, id := range hidden {
		unknown = strings.ReplaceAll(unknown, id, uuid.NewString())
	}
	if unknown == p.path {
		return // the route names the channel through one of its messages
	}
	if twin := p.send(c, unknown); twin.status != res.status || !bytes.Equal(twin.body, res.body) {
		t.Errorf("%s %s: a hidden channel answers %d %s, an unknown one %d %s", p.method, p.what, res.status, res.body, twin.status, twin.body)
	}
}

// channelRoutesNotProbed are the routes with a channel in their path that the
// probes leave out, each with the reason a private channel stays hidden there.
var channelRoutesNotProbed = map[string]string{
	"GET /api/admin/channels/{id}/webhooks":           "Administrator only: whoever passes sees every channel, everybody else gets 403 for any id (adminMatrix)",
	"POST /api/admin/channels/{id}/webhooks":          "Administrator only, as above",
	"GET /api/admin/channels/{id}/outgoing-webhooks":  "Administrator only, as above",
	"POST /api/admin/channels/{id}/outgoing-webhooks": "Administrator only, as above",

	"GET /api/bot/v1/channels/{channelID}/messages":                                      "bot token, no member session: TestBotAPIScope",
	"POST /api/bot/v1/channels/{channelID}/messages":                                     "bot token, as above",
	"GET /api/bot/v1/channels/{channelID}/messages/{messageID}":                          "bot token, as above",
	"PATCH /api/bot/v1/channels/{channelID}/messages/{messageID}":                        "bot token, as above",
	"DELETE /api/bot/v1/channels/{channelID}/messages/{messageID}":                       "bot token, as above",
	"PUT /api/bot/v1/channels/{channelID}/messages/{messageID}/reactions/{emoji}/@me":    "bot token, as above",
	"DELETE /api/bot/v1/channels/{channelID}/messages/{messageID}/reactions/{emoji}/@me": "bot token, as above",
	"POST /api/bot/v1/channels/{channelID}/upload":                                       "bot token, as above",
}

// requireEveryChannelRouteProbed walks the real router: a route that takes a
// channel id is either probed with a private channel or listed, with a reason,
// in channelRoutesNotProbed. A new channel route fails here until someone
// decides which.
func requireEveryChannelRouteProbed(t *testing.T, a *app, probes []channelProbe) {
	t.Helper()
	mounted := map[string]bool{}
	for _, r := range apiRoutes(t, a) {
		if !strings.Contains(r.pattern, "/channels/{") {
			continue
		}
		mounted[r.String()] = true
		shape := regexp.MustCompile("^" + strings.ReplaceAll(regexp.QuoteMeta(routeParam.ReplaceAllString(r.pattern, "\x00")), "\x00", "[^/]+") + "$")
		probed := slices.ContainsFunc(probes, func(p channelProbe) bool { return p.method == r.method && shape.MatchString(p.path) })
		reason, exempt := channelRoutesNotProbed[r.String()]
		switch {
		case probed && exempt:
			t.Errorf("%s is probed and exempt (%s): drop the exemption", r, reason)
		case !probed && !exempt:
			t.Errorf("%s takes a channel id: probe it with a private channel, or add it to channelRoutesNotProbed with the reason it needs none", r)
		}
	}
	if len(mounted) < 40 {
		t.Fatalf("walked only %d channel routes", len(mounted))
	}
	for route := range channelRoutesNotProbed {
		if !mounted[route] {
			t.Errorf("channelRoutesNotProbed names %s, which is not mounted", route)
		}
	}
}

// TestMembersCannotTouchEachOthersObjects has one member create everything an
// account owns or authors in a channel both can see, then lets another plain
// member try to change or remove each of them by id. Every attempt must be
// refused, and the object must still be there afterwards.
func TestMembersCannotTouchEachOthersObjects(t *testing.T) {
	a := newApp(t, false)
	owner := a.seedAdmin()
	alice, mallory := a.register(owner, "alice"), a.register(owner, "mallory")
	ch := a.createChannel(owner, "general", "text")
	forum := a.createChannel(owner, "forum", "forum")
	base := "/api/channels/" + ch.String()

	msg := a.send(alice, ch, "alice's words")
	var poll struct{ ID uuid.UUID }
	res := alice.post(base+"/polls", map[string]any{"question": "Q?", "options": []string{"a", "b"}})
	wantStatus(t, "poll", res, http.StatusCreated)
	res.decode(t, &poll)
	var post struct{ ID uuid.UUID }
	res = alice.post("/api/channels/"+forum.String()+"/posts", map[string]any{"title": "mine", "content": "body"})
	wantStatus(t, "post", res, http.StatusCreated)
	res.decode(t, &post)
	var att struct{ ID uuid.UUID }
	res = alice.upload(base+"/attachments", "file", "a.png", "image/png", pngBytes, nil)
	wantStatus(t, "attachment", res, http.StatusCreated)
	res.decode(t, &att)
	var rem struct{ ID uuid.UUID }
	res = alice.post("/api/me/reminders", map[string]any{"message_id": msg, "remind_at": time.Now().Add(time.Hour)})
	wantStatus(t, "reminder", res, http.StatusCreated)
	res.decode(t, &rem)
	var sched struct{ ID uuid.UUID }
	res = alice.post(base+"/scheduled", map[string]any{"content": "later", "send_at": time.Now().Add(time.Hour)})
	wantStatus(t, "scheduled", res, http.StatusCreated)
	res.decode(t, &sched)
	ev := a.coverEventAt(owner, time.Now().Add(48*time.Hour), nil)
	evBase := "/api/events/" + ev.ID.String()
	var com struct{ ID uuid.UUID }
	res = alice.post(evBase+"/comments", map[string]string{"content": "I bring snacks"})
	wantStatus(t, "comment", res, http.StatusCreated)
	res.decode(t, &com)
	wantStatus(t, "rsvp", alice.put(evBase+"/rsvp", map[string]string{"response": "going"}), http.StatusOK)
	var sessions struct{ Sessions []struct{ ID string } }
	alice.get("/api/auth/sessions").decode(t, &sessions)
	if len(sessions.Sessions) != 1 {
		t.Fatalf("sessions: %+v", sessions)
	}

	for _, c := range []struct {
		what, method, path string
		body               any
		want               int
	}{
		{"edit message", "PUT", base + "/messages/" + msg.String(), map[string]string{"content": "hacked"}, 403},
		{"delete message", "DELETE", base + "/messages/" + msg.String(), nil, 403},
		{"pin message", "PUT", base + "/messages/" + msg.String() + "/pin", nil, 403},
		{"end poll", "POST", base + "/messages/" + poll.ID.String() + "/poll/end", nil, 403},
		{"edit post", "PATCH", "/api/channels/" + forum.String() + "/posts/" + post.ID.String(), map[string]any{"title": "hacked"}, 403},
		{"delete attachment", "DELETE", base + "/attachments/" + att.ID.String(), nil, 404},
		{"delete reminder", "DELETE", "/api/me/reminders/" + rem.ID.String(), nil, 404},
		{"edit scheduled", "PATCH", "/api/me/scheduled/" + sched.ID.String(), map[string]any{"content": "hacked"}, 404},
		{"delete scheduled", "DELETE", "/api/me/scheduled/" + sched.ID.String(), nil, 404},
		{"edit comment", "PATCH", evBase + "/comments/" + com.ID.String(), map[string]string{"content": "hacked"}, 403},
		{"delete comment", "DELETE", evBase + "/comments/" + com.ID.String(), nil, 403},
		{"remove signup", "DELETE", evBase + "/signup/" + alice.user.ID.String(), nil, 403},
		{"revoke session", "DELETE", "/api/auth/sessions/" + sessions.Sessions[0].ID, nil, 404},
		{"edit event", "PATCH", evBase, map[string]any{"title": "hacked"}, 403},
		{"cancel event", "POST", evBase + "/cancel", nil, 403},
	} {
		if res := mallory.do(c.method, c.path, c.body, nil); res.status != c.want {
			t.Errorf("%s: %d %s, want %d", c.what, res.status, res.body, c.want)
		}
	}

	// Nothing changed.
	var n int
	for _, q := range []string{
		`SELECT count(*) FROM messages WHERE id = '` + msg.String() + `' AND content = 'alice''s words'`,
		`SELECT count(*) FROM message_reminders WHERE id = '` + rem.ID.String() + `'`,
		`SELECT count(*) FROM scheduled_messages WHERE id = '` + sched.ID.String() + `' AND content = 'later'`,
		`SELECT count(*) FROM event_comments WHERE id = '` + com.ID.String() + `' AND content = 'I bring snacks'`,
	} {
		if err := a.db.QueryRow(context.Background(), q).Scan(&n); err != nil || n != 1 {
			t.Errorf("%s: %d %v", q, n, err)
		}
	}
	wantStatus(t, "alice's session lives", alice.get("/api/auth/me"), http.StatusOK)
}

// TestMessageIdsAreBoundToTheChannelInThePath pairs a channel the caller has
// every right in with an object that lives in another channel, one they cannot
// see. A permission checked against the path's channel alone would let them
// act on the object anyway; each route must treat the pair as not found.
func TestMessageIdsAreBoundToTheChannelInThePath(t *testing.T) {
	f := newAccessFixture(t, false)
	// The outsider moderates the open channel (it may even manage messages
	// there) but is no one in the secret one.
	open := f.a.createChannel(f.owner, "open", "text")
	forumOpen := f.a.createChannel(f.owner, "open-forum", "forum")
	mod := createRole(t, f.owner, "Mods", perm.ManageMessages|perm.ManageChannels)
	assignRoles(t, f.owner, f.outsider, mod)
	secret := f.privateChannel(t, "staff", "text")
	forumSecret := f.privateChannel(t, "staff-forum", "forum")
	sb := "/api/channels/" + secret.String()
	ob := "/api/channels/" + open.String()

	msg := postMessage(t, f.owner, secret, map[string]any{"content": "secret words"})
	var poll struct{ ID uuid.UUID }
	res := f.owner.post(sb+"/polls", map[string]any{"question": "Q?", "options": []string{"a", "b"}})
	wantStatus(t, "poll", res, http.StatusCreated)
	res.decode(t, &poll)
	up := f.owner.upload(sb+"/attachments", "file", "a.png", "image/png", pngBytes, nil)
	wantStatus(t, "attachment", up, http.StatusCreated)
	var att struct{ ID uuid.UUID }
	up.decode(t, &att)
	var post struct{ ID uuid.UUID }
	res = f.owner.post("/api/channels/"+forumSecret.String()+"/posts", map[string]any{"title": "post", "content": "body"})
	wantStatus(t, "post", res, http.StatusCreated)
	res.decode(t, &post)
	var tag struct{ ID uuid.UUID }
	res = f.owner.post("/api/channels/"+forumSecret.String()+"/tags", map[string]any{"name": "t"})
	wantStatus(t, "tag", res, http.StatusCreated)
	res.decode(t, &tag)
	fo := "/api/channels/" + forumOpen.String()
	m := msg.String()

	for _, c := range []struct {
		what, method, path string
		body               any
	}{
		{"edit", "PUT", ob + "/messages/" + m, map[string]string{"content": "x"}},
		{"delete", "DELETE", ob + "/messages/" + m, nil},
		{"history", "GET", ob + "/messages/" + m + "/edits", nil},
		{"pin", "PUT", ob + "/messages/" + m + "/pin", nil},
		{"unpin", "DELETE", ob + "/messages/" + m + "/pin", nil},
		{"poll", "GET", ob + "/messages/" + poll.ID.String() + "/poll", nil},
		{"vote", "PUT", ob + "/messages/" + poll.ID.String() + "/poll/votes", map[string]any{"options": []int{0}}},
		{"end poll", "POST", ob + "/messages/" + poll.ID.String() + "/poll/end", nil},
		{"attachment", "DELETE", ob + "/attachments/" + att.ID.String(), nil},
		{"edit post", "PATCH", fo + "/posts/" + post.ID.String(), map[string]any{"title": "x"}},
		{"read post", "PUT", fo + "/posts/" + post.ID.String() + "/read", nil},
		{"edit tag", "PATCH", fo + "/tags/" + tag.ID.String(), map[string]any{"name": "x"}},
		{"delete tag", "DELETE", fo + "/tags/" + tag.ID.String(), nil},
		{"unread", "POST", ob + "/unread", map[string]any{"message_id": msg}},
		{"parent", "POST", ob + "/messages", map[string]any{"content": "x", "parent_id": msg}},
		{"reply", "POST", ob + "/messages", map[string]any{"content": "x", "reply_to_id": msg}},
		{"schedule parent", "POST", ob + "/scheduled", map[string]any{"content": "x", "send_at": time.Now().Add(time.Hour), "parent_id": msg}},
		{"order tags", "PUT", fo + "/tags/order", map[string]any{"tag_ids": []uuid.UUID{tag.ID}}},
	} {
		res := f.outsider.do(c.method, c.path, c.body, nil)
		if res.status != http.StatusNotFound && res.status != http.StatusBadRequest {
			t.Errorf("%s: %d %s, want 404/400", c.what, res.status, res.body)
		}
		if res.status == http.StatusBadRequest {
			t.Logf("%s: 400 %s", c.what, res.body)
		}
	}
	var n int
	if err := f.a.db.QueryRow(context.Background(), `SELECT count(*) FROM messages WHERE id = $1 AND content = 'secret words'`, msg).Scan(&n); err != nil || n != 1 {
		t.Fatalf("the secret message changed: %d %v", n, err)
	}
}

// TestEverySurfaceThatWritesHonoursTheHoldsOnAMember puts one member under each
// hold the permission model knows (a timeout, community rules not yet accepted,
// the new-member wait) and tries every route that adds or changes content. The
// holds work by taking permissions away, so a route that never asks the
// permission model would let the member through; each must answer 403. Reading,
// taking back one's own words and the private lists stay open.
func TestEverySurfaceThatWritesHonoursTheHoldsOnAMember(t *testing.T) {
	a := newApp(t, false)
	owner := a.seedAdmin()
	mod := a.quickMember(owner, "moddy")
	assignRoles(t, owner, mod, createRole(t, owner, "mods", perm.TimeoutMembers))
	text := a.createChannel(owner, "general", "text")
	forum := a.createChannel(owner, "forum", "forum")
	tb, fb := "/api/channels/"+text.String(), "/api/channels/"+forum.String()
	other := a.send(owner, text, "from the owner")
	var poll struct{ ID uuid.UUID }
	owner.post(tb+"/polls", map[string]any{"question": "Q?", "options": []string{"a", "b"}}).decode(t, &poll)
	ev := a.coverEventAt(owner, time.Now().Add(48*time.Hour), nil)

	// A member created before any hold exists has written content of their own.
	type fixture struct {
		who  *client
		mine uuid.UUID
		post uuid.UUID
		dm   uuid.UUID
		peer *client
	}
	setup := func(name string) *fixture {
		who, peer := a.quickMember(owner, name), a.quickMember(owner, name+"peer")
		f := &fixture{who: who, peer: peer, mine: a.send(who, text, "mine")}
		var p, d struct{ ID uuid.UUID }
		res := who.post(fb+"/posts", map[string]any{"title": "mine", "content": "body"})
		wantStatus(t, "post", res, http.StatusCreated)
		res.decode(t, &p)
		res = who.post("/api/dms", map[string]any{"user_ids": []uuid.UUID{peer.user.ID}})
		wantStatus(t, "dm", res, http.StatusCreated)
		res.decode(t, &d)
		f.post, f.dm = p.ID, d.ID
		return f
	}

	holds := []struct {
		name string
		put  func(f *fixture)
	}{
		{"timeout", func(f *fixture) {
			wantStatus(t, "timeout", mod.post("/api/admin/users/"+f.who.user.ID.String()+"/timeout", map[string]any{"duration_seconds": 600}), http.StatusOK)
		}},
		{"new member", func(f *fixture) {
			// Joined a minute ago, held for an hour.
			if _, err := a.db.Exec(context.Background(), `UPDATE users SET created_at = NOW() - INTERVAL '1 minute' WHERE id = $1`, f.who.user.ID); err != nil {
				t.Fatal(err)
			}
			wantStatus(t, "raid", putRaid(t, owner, 0, 60, 60, 0), http.StatusOK)
		}},
		{"rules", func(f *fixture) {
			wantStatus(t, "rules", owner.put("/api/admin/onboarding", map[string]any{"community_name": "Home", "welcome_text": "", "rules_text": "Be kind."}), http.StatusOK)
		}},
	}
	fixtures := make([]*fixture, len(holds))
	for i := range holds {
		fixtures[i] = setup(fmt.Sprintf("held%d", i))
	}
	for i, h := range holds {
		f := fixtures[i]
		h.put(f)
		c := f.who
		dm := "/api/channels/" + f.dm.String()
		for _, p := range []struct {
			what, method, path string
			body               any
		}{
			{"message", "POST", tb + "/messages", map[string]any{"content": "x"}},
			{"reply", "POST", tb + "/messages", map[string]any{"content": "x", "parent_id": other}},
			{"edit own", "PUT", tb + "/messages/" + f.mine.String(), map[string]any{"content": "y"}},
			{"poll", "POST", tb + "/polls", map[string]any{"question": "Q?", "options": []string{"a", "b"}}},
			{"vote", "PUT", tb + "/messages/" + poll.ID.String() + "/poll/votes", map[string]any{"options": []int{0}}},
			{"scheduled", "POST", tb + "/scheduled", map[string]any{"content": "x", "send_at": time.Now().Add(time.Hour)}},
			{"forward", "POST", "/api/messages/" + other.String() + "/forward", map[string]any{"channel_id": text}},
			{"new post", "POST", fb + "/posts", map[string]any{"title": "x", "content": "y"}},
			{"edit post", "PATCH", fb + "/posts/" + f.post.String(), map[string]any{"title": "z"}},
			{"dm message", "POST", dm + "/messages", map[string]any{"content": "x"}},
			{"reaction", "POST", "/api/messages/" + other.String() + "/reactions", map[string]string{"emoji": "🔥"}},
			{"event comment", "POST", "/api/events/" + ev.ID.String() + "/comments", map[string]string{"content": "x"}},
			{"upload", "POST", tb + "/upload", nil},
			{"attachment", "POST", tb + "/attachments", nil},
		} {
			var res response
			if p.what == "upload" || p.what == "attachment" {
				res = c.upload(p.path, "file", "a.png", "image/png", pngBytes, nil)
			} else {
				res = c.do(p.method, p.path, p.body, nil)
			}
			if res.status != http.StatusForbidden {
				t.Errorf("%s: %s while held: %d %s, want 403", h.name, p.what, res.status, res.body)
			}
		}
		// What a held member keeps.
		wantStatus(t, h.name+": read", c.get(tb+"/messages"), http.StatusOK)
		wantStatus(t, h.name+": report", c.post("/api/messages/"+other.String()+"/report", map[string]any{"category": "spam"}), http.StatusCreated)
		wantStatus(t, h.name+": save", c.put("/api/me/saved/"+other.String(), nil), http.StatusOK)
		wantStatus(t, h.name+": profile", c.put("/api/users/me/profile", map[string]any{"display_name": "held"}), http.StatusOK)
		if h.name == "timeout" {
			wantStatus(t, h.name+": delete own", c.delete(tb+"/messages/"+f.mine.String()), http.StatusNoContent)
		}
	}
}

// TestNoRouteLetsTheOwnerOrAModeratorIntoAConversation walks the channel-scoped
// routes with the id of a direct message conversation between two members. The
// Owner holds every permission, and a moderator may manage messages server-wide,
// but a conversation belongs to its members alone: every route must answer as
// for a channel that does not exist, and nothing in it may change.
func TestNoRouteLetsTheOwnerOrAModeratorIntoAConversation(t *testing.T) {
	f := newDMFixture(t, false)
	mod := f.a.quickMember(f.admin, "modo")
	assignRoles(t, f.admin, mod, createRole(t, f.admin, "mods", perm.ManageMessages|perm.DisconnectMembers|perm.ManageChannels|perm.ManageReports))
	dm := f.alice.mustDM(t, f.bob)
	base := "/api/channels/" + dm.ID.String()
	wantStatus(t, "alice writes", f.alice.dmPost(dm.ID, "private words"), http.StatusCreated)
	msg := f.alice.dmPost(dm.ID, "second")
	var m struct{ ID uuid.UUID }
	msg.decode(t, &m)
	wantStatus(t, "bob answers", f.bob.dmPost(dm.ID, "answer"), http.StatusCreated)
	var poll struct{ ID uuid.UUID }
	res := f.alice.post(base+"/polls", map[string]any{"question": "Q?", "options": []string{"a", "b"}})
	wantStatus(t, "poll", res, http.StatusCreated)
	res.decode(t, &poll)
	id := m.ID.String()

	for who, c := range map[string]*client{"owner": f.admin, "moderator": mod} {
		for _, p := range []struct {
			what, method, path string
			body               any
		}{
			{"bulk delete", "POST", base + "/messages/bulk-delete", map[string]any{"user_id": f.alice.user.ID, "count": 10}},
			{"pin", "PUT", base + "/messages/" + id + "/pin", nil},
			{"unpin", "DELETE", base + "/messages/" + id + "/pin", nil},
			{"pins", "GET", base + "/pins", nil},
			{"end poll", "POST", base + "/messages/" + poll.ID.String() + "/poll/end", nil},
			{"poll", "GET", base + "/messages/" + poll.ID.String() + "/poll", nil},
			{"vote", "PUT", base + "/messages/" + poll.ID.String() + "/poll/votes", map[string]any{"options": []int{0}}},
			{"thread", "GET", "/api/messages/" + id + "/thread", nil},
			{"report", "POST", "/api/messages/" + id + "/report", map[string]any{"category": "spam"}},
			{"forward", "GET", "/api/messages/" + id + "/forward", nil},
			{"save", "PUT", "/api/me/saved/" + id, nil},
			{"follow", "PUT", "/api/threads/" + id + "/follow", nil},
			{"edits", "GET", base + "/messages/" + id + "/edits", nil},
			{"first unread", "GET", base + "/first-unread", nil},
			{"commands", "GET", base + "/commands", nil},
			{"scheduled", "POST", base + "/scheduled", map[string]any{"content": "x", "send_at": time.Now().Add(time.Hour)}},
			{"notifications", "PUT", base + "/notifications", map[string]string{"level": "mute"}},
			{"talk", "POST", "/api/dms/" + dm.ID.String() + "/talk", nil},
			{"hide", "PUT", "/api/dms/" + dm.ID.String() + "/hidden", map[string]any{"hidden": true}},
			{"members", "POST", "/api/dms/" + dm.ID.String() + "/members", map[string]any{"user_id": f.carol.user.ID}},
			{"voice disconnect", "POST", base + "/voice/" + f.alice.user.ID.String() + "/disconnect", nil},
			{"search", "GET", "/api/search?channel_id=" + dm.ID.String(), nil},
		} {
			res := c.do(p.method, p.path, p.body, nil)
			if res.status != http.StatusNotFound {
				t.Errorf("%s: %s: %d %s, want 404", who, p.what, res.status, res.body)
			}
		}
	}
	var n int
	if err := f.a.db.QueryRow(context.Background(), `SELECT count(*) FROM messages WHERE channel_id = $1`, dm.ID).Scan(&n); err != nil || n != 4 {
		t.Fatalf("messages left in the conversation: %d %v", n, err)
	}
	var audited int
	if err := f.a.db.QueryRow(context.Background(), `SELECT count(*) FROM audit_log WHERE details::text ILIKE '%private words%'`).Scan(&audited); err != nil || audited != 0 {
		t.Fatalf("the audit log holds the private words: %d %v", audited, err)
	}
}

// TestManageChannelsDoesNotReachAChannelTheMemberCannotSee: managing channels
// is a server-wide power, but a channel hidden from the holder is not found by
// any of the management routes, so it cannot be renamed, emptied, copied (with
// its permission overwrites) or deleted blind.
func TestManageChannelsDoesNotReachAChannelTheMemberCannotSee(t *testing.T) {
	f := newAccessFixture(t, false)
	builder := f.a.quickMember(f.owner, "builder")
	assignRoles(t, f.owner, builder, createRole(t, f.owner, "Builder", perm.ManageChannels))
	secret := f.privateChannel(t, "staff-only", "text")
	postMessage(t, f.owner, secret, map[string]any{"content": "classified"})
	id := secret.String()
	for _, p := range []struct {
		what, method, path string
		body               any
	}{
		{"get overwrites", "GET", "/api/admin/channels/" + id + "/overwrites", nil},
		{"put overwrites", "PUT", "/api/admin/channels/" + id + "/overwrites", map[string]any{"overwrites": []any{}}},
		{"rename", "PATCH", "/api/admin/channels/" + id, map[string]any{"name": "renamed"}},
		{"auto delete", "PUT", "/api/admin/channels/" + id + "/auto-delete", map[string]any{"days": 1}},
		{"duplicate", "POST", "/api/admin/channels/" + id + "/duplicate", map[string]any{}},
		{"delete", "DELETE", "/api/admin/channels/" + id, nil},
		{"messages", "GET", "/api/channels/" + id + "/messages", nil},
	} {
		if res := builder.do(p.method, p.path, p.body, nil); res.status != http.StatusNotFound {
			t.Errorf("%s: %d %s, want 404", p.what, res.status, res.body)
		}
	}
	var name string
	if err := f.a.db.QueryRow(context.Background(), `SELECT name FROM channels WHERE id = $1`, secret).Scan(&name); err != nil || name != "staff-only" {
		t.Fatalf("the channel changed: %q %v", name, err)
	}
}

// TestEventsOfAPrivateTalkAreInvisibleToOutsiders: an event located in a Talk
// follows the Talk's visibility on every route that takes the event's id, and
// someone who manages events but cannot see the Talk cannot pin an event to it.
func TestEventsOfAPrivateTalkAreInvisibleToOutsiders(t *testing.T) {
	f := newAccessFixture(t, false)
	talk := f.privateChannel(t, "staff-talk", "voice")
	res := f.owner.post("/api/events", map[string]any{
		"title": "Staff meeting", "description": "agenda", "start_time": time.Now().Add(48 * time.Hour), "channel_id": talk,
	})
	wantStatus(t, "event", res, http.StatusCreated)
	var ev struct{ ID uuid.UUID }
	res.decode(t, &ev)
	base := "/api/events/" + ev.ID.String()
	wantStatus(t, "insider comments", f.insider.post(base+"/comments", map[string]string{"content": "see you"}), http.StatusCreated)

	out := f.outsider
	var list []struct{ ID uuid.UUID }
	out.get("/api/events").decode(t, &list)
	for _, e := range list {
		if e.ID == ev.ID {
			t.Fatal("the outsider's event list holds an event of a private Talk")
		}
	}
	for _, p := range []struct {
		what, method, path string
		body               any
	}{
		{"get", "GET", base, nil},
		{"comments", "GET", base + "/comments", nil},
		{"comment", "POST", base + "/comments", map[string]string{"content": "x"}},
		{"mute comments", "PUT", base + "/comments/mute", map[string]any{"muted": true}},
		{"rsvp", "PUT", base + "/rsvp", map[string]string{"response": "going"}},
		{"rsvps", "GET", base + "/rsvps", nil},
		{"unrsvp", "DELETE", base + "/rsvp", nil},
		{"signup", "PUT", base + "/signup", map[string]any{}},
	} {
		if res := out.do(p.method, p.path, p.body, nil); res.status != http.StatusNotFound {
			t.Errorf("%s as an outsider: %d %s, want 404", p.what, res.status, res.body)
		}
	}

	// A member who manages events but cannot see the Talk cannot attach events to it.
	planner := f.a.quickMember(f.owner, "planner")
	assignRoles(t, f.owner, planner, createRole(t, f.owner, "Planner", perm.ManageEvents))
	for what, res := range map[string]response{
		"create": planner.post("/api/events", map[string]any{"title": "x", "description": "", "start_time": time.Now().Add(48 * time.Hour), "channel_id": talk}),
		"edit":   planner.patch("/api/events/"+ev.ID.String(), map[string]any{"title": "renamed"}),
	} {
		if res.status < 400 {
			t.Errorf("planner %s: %d %s, want a refusal", what, res.status, res.body)
		}
	}
}
