//go:build integration

package server

import (
	"testing"
	"time"
)

func TestSendingAMessageMarksTheChannelReadUpToIt(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	ch := a.createChannel(admin, "lobby", "text")
	max := a.register(admin, "max")

	// No marker yet: three messages by others are unread, and writing clears them.
	for i := 0; i < 3; i++ {
		postMessage(t, admin, ch, map[string]any{"content": "news"})
	}
	if got := max.readState(t, ch); got.UnreadCount != 3 {
		t.Fatalf("before sending: unread=%d, want 3", got.UnreadCount)
	}
	root := postMessage(t, max, ch, map[string]any{"content": "my answer"})
	if got := max.readState(t, ch); got.UnreadCount != 0 || got.MentionCount != 0 {
		t.Fatalf("after sending: %+v, want nothing unread", got)
	}

	// A marker exists and has fallen behind: sending moves it up to the message.
	postMessage(t, admin, ch, map[string]any{"content": "more", "parent_id": root})
	postMessage(t, admin, ch, map[string]any{"content": "and more"})
	postMessage(t, admin, ch, map[string]any{"content": "@max look"})
	if got := max.readState(t, ch); got.UnreadCount != 2 || got.MentionCount != 1 {
		t.Fatalf("before second send: %+v", got)
	}
	// A thread reply is not a channel message: it leaves the channel unread.
	postMessage(t, max, ch, map[string]any{"content": "in the thread", "parent_id": root})
	if got := max.readState(t, ch); got.UnreadCount != 2 {
		t.Fatalf("a thread reply cleared the channel: %+v", got)
	}
	time.Sleep(10 * time.Millisecond)
	postMessage(t, max, ch, map[string]any{"content": "caught up"})
	if got := max.readState(t, ch); got.UnreadCount != 0 || got.MentionCount != 0 {
		t.Fatalf("after second send: %+v", got)
	}

	// What others write afterwards counts again.
	postMessage(t, admin, ch, map[string]any{"content": "later"})
	if got := max.readState(t, ch); got.UnreadCount != 1 {
		t.Fatalf("after news: %+v, want 1 unread", got)
	}
}

func TestReplyingInAConversationMarksItRead(t *testing.T) {
	f := newDMFixture(t, false)
	dm := f.alice.mustDM(t, f.bob)
	f.alice.dmPost(dm.ID, "one")
	f.alice.dmPost(dm.ID, "two")

	unread := func() int {
		var list []dmView
		f.bob.get("/api/dms").decode(t, &list)
		for _, d := range list {
			if d.ID == dm.ID {
				return d.UnreadCount
			}
		}
		t.Fatal("conversation missing from the list")
		return -1
	}
	if got := unread(); got != 2 {
		t.Fatalf("before replying: unread=%d, want 2", got)
	}
	if res := f.bob.dmPost(dm.ID, "hi"); res.status/100 != 2 {
		t.Fatalf("reply: %d %s", res.status, res.body)
	}
	if got := unread(); got != 0 {
		t.Fatalf("after replying: unread=%d, want 0", got)
	}
	f.alice.dmPost(dm.ID, "three")
	if got := unread(); got != 1 {
		t.Fatalf("after news: unread=%d, want 1", got)
	}
}
