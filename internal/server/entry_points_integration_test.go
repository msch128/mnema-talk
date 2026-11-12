//go:build integration

package server

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
)

// One rule, many doors (2026-11 defect hunt, lane 3, pair 6): the checks that
// stop a new message also have to stop every other way of putting words in
// front of someone. Each row is an operation through its real route after the
// condition was set up; want is the status the documents require.

type doorCell struct {
	name string
	got  func() response
	want int
}

func runCells(t *testing.T, cells []doorCell) {
	t.Helper()
	for _, c := range cells {
		if res := c.got(); res.status != c.want {
			t.Errorf("%s: %d %s, want %d", c.name, res.status, res.body, c.want)
		}
	}
}

func msgID(t *testing.T, res response) uuid.UUID {
	t.Helper()
	if res.status != http.StatusCreated {
		t.Fatalf("post: %d %s", res.status, res.body)
	}
	var m struct{ ID uuid.UUID }
	res.decode(t, &m)
	return m.ID
}

// A block makes an existing 1:1 conversation unwritable in both directions
// (doc/architecture.md "Starting and writing", doc/moderation.md "Blocking a member").
func TestABlockClosesTheDoorsOfAConversation(t *testing.T) {
	f := newDMFixture(t, false)
	dm := f.alice.mustDM(t, f.bob)
	msgID(t, f.alice.dmPost(dm.ID, "hello bob"))
	theirs := msgID(t, f.bob.dmPost(dm.ID, "hi alice"))
	general := f.a.createChannel(f.admin, "general", "text")
	public := f.a.send(f.admin, general, "something to forward")
	wantStatus(t, "block", f.bob.put("/api/me/blocks/"+f.alice.user.ID.String(), nil), http.StatusNoContent)
	runCells(t, []doorCell{
		{"blocked member posts", func() response { return f.alice.dmPost(dm.ID, "still there?") }, http.StatusForbidden},
		{"blocked member reacts", func() response {
			return f.alice.post("/api/messages/"+theirs.String()+"/reactions", map[string]string{"emoji": "👍"})
		}, http.StatusForbidden},
		{"blocked member forwards into the conversation", func() response {
			return f.alice.post("/api/messages/"+public.String()+"/forward", map[string]any{"channel_id": dm.ID})
		}, http.StatusForbidden},
		{"blocked member starts a Talk (rings the blocker)", func() response {
			return f.alice.post("/api/dms/"+dm.ID.String()+"/talk", nil)
		}, http.StatusForbidden},
		{"blocker posts", func() response { return f.bob.dmPost(dm.ID, "one more thing") }, http.StatusForbidden},
	})
}

// A locked forum post: only members with Manage messages reply (doc/forums.md).
func TestALockedPostClosesTheDoorsForMembers(t *testing.T) {
	f := newDMFixture(t, false)
	forum := f.a.createChannel(f.admin, "board", "forum")
	fb := "/api/channels/" + forum.String()
	var post struct{ ID uuid.UUID }
	res := f.alice.post(fb+"/posts", map[string]any{"title": "my post", "content": "body"})
	wantStatus(t, "post", res, http.StatusCreated)
	res.decode(t, &post)
	reply := msgID(t, f.bob.post(fb+"/messages", map[string]any{"content": "a reply", "parent_id": post.ID}))
	wantStatus(t, "lock", f.admin.patch(fb+"/posts/"+post.ID.String(), map[string]any{"is_locked": true}), http.StatusOK)
	runCells(t, []doorCell{
		{"reply", func() response {
			return f.bob.post(fb+"/messages", map[string]any{"content": "more", "parent_id": post.ID})
		}, http.StatusForbidden},
		{"author edits the title", func() response {
			return f.alice.patch(fb+"/posts/"+post.ID.String(), map[string]any{"title": "new title"})
		}, http.StatusForbidden},
	})
	knownLockedEdit(t, f.bob, fb+"/messages/"+reply.String())
}

// knownLockedEdit asserts the WRONG behaviour measured by the hunt (finding
// H-L3-08, an inverse assertion): a member rewrites their reply in a locked
// post. It fails the day the edit is refused; then make it a cell above.
func knownLockedEdit(t *testing.T, c *client, path string) {
	t.Helper()
	if res := c.put(path, map[string]any{"content": "rewritten after the lock"}); res.status != http.StatusOK {
		t.Errorf("edit of a reply in a locked post: %d, H-L3-08 is fixed: want 403 in the table", res.status)
	}
}

// A timeout takes Send messages and Send direct messages "so every surface
// honours it" (doc/moderation.md, Timeouts); the existing timeout test covers
// post, reply, edit, reaction and upload, these are the other doors.
func TestATimeoutClosesTheOtherDoorsToo(t *testing.T) {
	f := newDMFixture(t, false)
	general := f.a.createChannel(f.admin, "general", "text")
	forum := f.a.createChannel(f.admin, "board", "forum")
	clean := f.a.send(f.admin, general, "a message")
	dm := f.alice.mustDM(t, f.bob)
	msgID(t, f.bob.dmPost(dm.ID, "hi alice"))
	ev := f.a.coverEventAt(f.admin, time.Now().Add(48*time.Hour), nil)
	wantStatus(t, "timeout", f.admin.post("/api/admin/users/"+f.alice.user.ID.String()+"/timeout", map[string]any{"duration_seconds": 600, "reason": "x"}), http.StatusOK)
	ch := "/api/channels/" + general.String()
	runCells(t, []doorCell{
		{"forward", func() response {
			return f.alice.post("/api/messages/"+clean.String()+"/forward", map[string]any{"channel_id": general})
		}, http.StatusForbidden},
		{"poll", func() response {
			return f.alice.post(ch+"/polls", map[string]any{"question": "q?", "options": []string{"a", "b"}})
		}, http.StatusForbidden},
		{"schedule a message", func() response {
			return f.alice.post(ch+"/scheduled", map[string]any{"content": "later", "send_at": time.Now().Add(time.Hour)})
		}, http.StatusForbidden},
		{"forum post", func() response {
			return f.alice.post("/api/channels/"+forum.String()+"/posts", map[string]any{"title": "t", "content": "c"})
		}, http.StatusForbidden},
		{"direct message", func() response { return f.alice.dmPost(dm.ID, "hello") }, http.StatusForbidden},
		{"Talk from a conversation", func() response { return f.alice.post("/api/dms/"+dm.ID.String()+"/talk", nil) }, http.StatusForbidden},
		{"event comment", func() response {
			return f.alice.post("/api/events/"+ev.ID.String()+"/comments", map[string]string{"content": "hi"})
		}, http.StatusForbidden},
		{"new conversation", func() response { return f.alice.startDM(f.carol) }, http.StatusForbidden},
	})
}

// "Automod covers every way a member's message is created: plain messages,
// uploads, thread replies, polls, forwards, slash command replies and edits"
// (doc/moderation.md, Automod).
func TestABlockedWordIsRefusedAtEveryDoorOfAMessage(t *testing.T) {
	f := newAutomodFixture(t)
	f.age(f.member)
	f.addRule(wordRule("No spam", "block", "spam"))
	ch := "/api/channels/" + f.general.String()
	clean := f.a.send(f.admin, f.general, "a clean message")
	mine := f.a.send(f.member, f.general, "my clean words")
	forum := f.a.createChannel(f.admin, "board", "forum")
	fb := "/api/channels/" + forum.String()
	var post struct{ ID uuid.UUID }
	res := f.member.post(fb+"/posts", map[string]any{"title": "clean title", "content": "clean body"})
	wantStatus(t, "forum post", res, http.StatusCreated)
	res.decode(t, &post)
	runCells(t, []doorCell{
		{"message", func() response { return f.say(f.member, f.general, "buy spam") }, http.StatusBadRequest},
		{"thread reply", func() response {
			return f.member.post(ch+"/messages", map[string]any{"content": "spam here", "parent_id": clean})
		}, http.StatusBadRequest},
		{"edit", func() response {
			return f.member.put(ch+"/messages/"+mine.String(), map[string]any{"content": "now spam"})
		}, http.StatusBadRequest},
		{"forward comment", func() response {
			return f.member.post("/api/messages/"+clean.String()+"/forward", map[string]any{"channel_id": f.general, "comment": "spam"})
		}, http.StatusBadRequest},
		{"poll question", func() response {
			return f.member.post(ch+"/polls", map[string]any{"question": "spam?", "options": []string{"a", "b"}})
		}, http.StatusBadRequest},
		{"poll option", func() response {
			return f.member.post(ch+"/polls", map[string]any{"question": "lunch?", "options": []string{"spam", "eggs"}})
		}, http.StatusBadRequest},
		{"upload caption", func() response {
			return f.member.upload(ch+"/upload", "file", "a.png", "image/png", pngBytes, map[string]string{"content": "spam"})
		}, http.StatusBadRequest},
		{"forum post title", func() response {
			return f.member.post(fb+"/posts", map[string]any{"title": "spam", "content": "body"})
		}, http.StatusBadRequest},
		{"forum title edit", func() response {
			return f.member.patch(fb+"/posts/"+post.ID.String(), map[string]any{"title": "spam"})
		}, http.StatusBadRequest},
	})
}
