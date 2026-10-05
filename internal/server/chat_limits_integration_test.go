//go:build integration

package server

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat"
)

func TestReactionLimits(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	ch := a.createChannel(admin, "general", "text")
	msg := a.send(admin, ch, "react to me")
	path := "/api/messages/" + msg.String() + "/reactions"
	react := func(c *client, emoji string) response {
		return c.post(path, map[string]string{"emoji": emoji})
	}

	for i := 0; i < chat.MaxReactionsPerUser; i++ {
		if res := react(admin, fmt.Sprintf("e%d", i)); res.status != http.StatusOK {
			t.Fatalf("reaction %d: %d %s", i, res.status, res.body)
		}
	}
	res := react(admin, "one-too-many")
	if res.status != http.StatusConflict || res.code(t) != "CONFLICT" {
		t.Fatalf("reaction beyond the per-user cap: %d %s", res.status, res.body)
	}
	// Removing one is always allowed, and frees a slot.
	if res := react(admin, "e0"); res.status != http.StatusOK {
		t.Fatalf("remove reaction: %d %s", res.status, res.body)
	}
	if res := react(admin, "e0"); res.status != http.StatusOK {
		t.Fatalf("re-add after removing: %d %s", res.status, res.body)
	}

	// Distinct emoji per message: two more users fill the message up to 50.
	b, c := a.register(admin, "bert"), a.register(admin, "carla")
	for i := 0; i < chat.MaxReactionsPerUser; i++ {
		if res := react(b, fmt.Sprintf("b%d", i)); res.status != http.StatusOK {
			t.Fatalf("bert reaction %d: %d %s", i, res.status, res.body)
		}
	}
	for i := 0; i < chat.MaxEmojiPerMessage-2*chat.MaxReactionsPerUser; i++ {
		if res := react(c, fmt.Sprintf("c%d", i)); res.status != http.StatusOK {
			t.Fatalf("carla reaction %d: %d %s", i, res.status, res.body)
		}
	}
	if res := react(c, "brand-new"); res.status != http.StatusConflict {
		t.Fatalf("new emoji beyond the per-message cap: %d %s", res.status, res.body)
	}
	// Joining an existing emoji does not add a distinct one.
	res = react(c, "e1")
	if res.status != http.StatusOK {
		t.Fatalf("join existing emoji: %d %s", res.status, res.body)
	}
	var out chat.ReactionResult
	res.decode(t, &out)
	if len(out.Reactions) != chat.MaxEmojiPerMessage || out.MessageID != msg {
		t.Fatalf("summary has %d emoji for %s", len(out.Reactions), out.MessageID)
	}

	if res := admin.post("/api/messages/"+uuid.NewString()+"/reactions", map[string]string{"emoji": "x"}); res.status != http.StatusNotFound {
		t.Fatalf("reaction on unknown message: %d", res.status)
	}
}

func TestThreadOfAReplyIsNotFound(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	ch := a.createChannel(admin, "general", "text")
	root := a.send(admin, ch, "root")
	res := admin.post("/api/channels/"+ch.String()+"/messages", map[string]any{"content": "reply", "parent_id": root})
	if res.status != http.StatusCreated {
		t.Fatalf("reply: %d %s", res.status, res.body)
	}
	var reply struct{ ID uuid.UUID }
	res.decode(t, &reply)

	if res := admin.get("/api/messages/" + root.String() + "/thread"); res.status != http.StatusOK {
		t.Fatalf("thread of root: %d %s", res.status, res.body)
	}
	if res := admin.get("/api/messages/" + reply.ID.String() + "/thread"); res.status != http.StatusNotFound {
		t.Fatalf("thread of a reply: %d %s", res.status, res.body)
	}
}

func TestEditToEmptyOnlyWithAttachment(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	ch := a.createChannel(admin, "general", "text")
	base := "/api/channels/" + ch.String() + "/messages/"

	text := a.send(admin, ch, "words")
	if res := admin.put(base+text.String(), map[string]string{"content": "  "}); res.status != http.StatusBadRequest {
		t.Fatalf("empty edit of a text message: %d %s", res.status, res.body)
	}

	up := admin.upload("/api/channels/"+ch.String()+"/upload", "file", "a.png", "image/png", pngBytes, map[string]string{"content": "caption @Herzog"})
	if up.status != http.StatusCreated {
		t.Fatalf("upload: %d %s", up.status, up.body)
	}
	var m chat.Message
	up.decode(t, &m)
	res := admin.put(base+m.ID.String(), map[string]string{"content": ""})
	if res.status != http.StatusOK {
		t.Fatalf("empty edit of an upload: %d %s", res.status, res.body)
	}
	var edited chat.Message
	res.decode(t, &edited)
	if edited.Content != "" || !edited.IsEdited || len(edited.Attachments) != 1 {
		t.Fatalf("edited upload = %+v", edited)
	}
	if len(edited.Mentions) != 0 {
		t.Fatalf("mentions not updated with the edit: %v", edited.Mentions)
	}
}

func memberCount(t *testing.T, c *client, id uuid.UUID) int64 {
	t.Helper()
	var members []auth.User
	c.get("/api/members").decode(t, &members)
	for _, m := range members {
		if m.ID == id {
			return m.MessageCount
		}
	}
	t.Fatalf("member %s not listed", id)
	return 0
}

// message_count follows posts, uploads and deletes, cascades included.
func TestMemberMessageCountStaysCurrent(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	max := a.register(admin, "max")
	ch := a.createChannel(admin, "general", "text")
	other := a.createChannel(admin, "other", "text")

	root := a.send(max, ch, "one")
	if res := max.post("/api/channels/"+ch.String()+"/messages", map[string]any{"content": "two", "parent_id": root}); res.status != http.StatusCreated {
		t.Fatalf("reply: %d", res.status)
	}
	if res := max.upload("/api/channels/"+ch.String()+"/upload", "file", "a.png", "image/png", pngBytes, nil); res.status != http.StatusCreated {
		t.Fatalf("upload: %d %s", res.status, res.body)
	}
	a.send(max, other, "elsewhere")
	if got := memberCount(t, admin, max.user.ID); got != 4 {
		t.Fatalf("message_count = %d, want 4", got)
	}

	// Deleting the root takes its reply along.
	if res := max.delete("/api/channels/" + ch.String() + "/messages/" + root.String()); res.status != http.StatusNoContent {
		t.Fatalf("delete: %d", res.status)
	}
	if got := memberCount(t, admin, max.user.ID); got != 2 {
		t.Fatalf("message_count after deleting a thread = %d, want 2", got)
	}
	if res := admin.delete("/api/admin/channels/" + ch.String()); res.status != http.StatusNoContent {
		t.Fatalf("delete channel: %d", res.status)
	}
	if got := memberCount(t, admin, max.user.ID); got != 1 {
		t.Fatalf("message_count after deleting a channel = %d, want 1", got)
	}
	if a.store.Len() != 0 {
		t.Fatalf("%d objects left after the channel's deletion", a.store.Len())
	}
}

func TestDeleteMessageErrors(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	max := a.register(admin, "max")
	ch := a.createChannel(admin, "general", "text")
	msg := a.send(admin, ch, "mine")
	base := "/api/channels/" + ch.String() + "/messages/"
	if res := max.delete(base + msg.String()); res.status != http.StatusForbidden {
		t.Fatalf("delete someone else's message: %d", res.status)
	}
	if res := admin.delete(base + msg.String()); res.status != http.StatusNoContent {
		t.Fatalf("delete own message: %d", res.status)
	}
	if res := admin.delete(base + msg.String()); res.status != http.StatusNotFound {
		t.Fatalf("delete twice: %d", res.status)
	}
}
