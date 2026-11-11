//go:build integration

package server

import (
	"net/http"
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

func TestABuiltInTextCommandMarksTheChannelReadLikeATypedMessage(t *testing.T) {
	f := newBuiltinsFixture(t)
	for i := 0; i < 2; i++ {
		postMessage(t, f.owner, f.text, map[string]any{"content": "news"})
	}
	if got := f.member.readState(t, f.text); got.UnreadCount != 2 {
		t.Fatalf("before /shrug: unread=%d, want 2", got.UnreadCount)
	}
	wantStatus(t, "/shrug", f.run(f.member, "shrug", map[string]any{"text": "dunno"}), http.StatusOK)
	if got := f.member.readState(t, f.text); got.UnreadCount != 0 {
		t.Fatalf("after /shrug: unread=%d, want 0", got.UnreadCount)
	}
}

func TestPostingAPollMarksTheChannelRead(t *testing.T) {
	f := newPollFixture(t)
	unreadAfter := func(name string, post func()) {
		t.Helper()
		for i := 0; i < 2; i++ {
			postMessage(t, f.owner, f.channel, map[string]any{"content": "news"})
		}
		if got := f.alice.readState(t, f.channel); got.UnreadCount != 2 {
			t.Fatalf("before %s: unread=%d, want 2", name, got.UnreadCount)
		}
		post()
		if got := f.alice.readState(t, f.channel); got.UnreadCount != 0 {
			t.Fatalf("after %s: unread=%d, want 0", name, got.UnreadCount)
		}
	}
	unreadAfter("the REST route", func() { f.basic(t, f.alice, nil) })
	unreadAfter("/poll", func() {
		wantStatus(t, "run /poll", slashPoll(f.alice, f.channel, map[string]any{"question": "Again?", "answers": "Yes | No"}), http.StatusOK)
	})
}

// The author of a scheduled message is not reading the channel when the
// server sends it: what others wrote since their last visit stays unread.
func TestAScheduledMessageLeavesWhatTheAuthorHasNotReadUnread(t *testing.T) {
	f := newLaterFixture(t)
	wantStatus(t, "schedule", f.schedule(f.alice, f.channel, "good morning", inFuture(time.Hour)), http.StatusCreated)
	for i := 0; i < 2; i++ {
		postMessage(t, f.owner, f.channel, map[string]any{"content": "news"})
	}
	f.sweep(t)
	if len(f.history(t, f.alice, f.channel)) != 3 {
		t.Fatal("the scheduled message was not sent")
	}
	if got := f.alice.readState(t, f.channel); got.UnreadCount != 2 {
		t.Fatalf("after the scheduled send: unread=%d, want 2", got.UnreadCount)
	}
}
