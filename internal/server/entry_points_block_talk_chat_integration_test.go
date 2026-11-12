//go:build integration

package server

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat/talks"
)

// The Talk of a conversation of two belongs to the conversation
// (doc/talks.md), and so does its chat: after a block, every write door of a
// Talk opened before the block answers exactly like the conversation's
// (doc/moderation.md "Blocking a member"), while the Talk itself stays open
// (closing it would tell the blocked member). Finding H-L3-12 of the 2026-11
// hunt.
func TestABlockClosesTheDoorsOfTheConversationsTalkChat(t *testing.T) {
	f := newDMFixture(t, true)
	dm := f.alice.mustDM(t, f.bob)
	msgID(t, f.alice.dmPost(dm.ID, "hello bob"))
	msgID(t, f.bob.dmPost(dm.ID, "hi alice"))
	talk := startTalk(t, f.alice, dm.ID)
	chat := "/api/channels/" + talk.String() + "/messages"
	mine := msgID(t, f.alice.post(chat, map[string]any{"content": "in the call chat"}))
	theirs := msgID(t, f.bob.post(chat, map[string]any{"content": "me too"}))
	poll, option := dmPoll(t, f.bob, talk)
	general := f.a.createChannel(f.admin, "general", "text")
	public := f.a.send(f.admin, general, "something to forward")
	wantStatus(t, "schedule", f.alice.post("/api/channels/"+talk.String()+"/scheduled",
		map[string]any{"content": "later words", "send_at": time.Now().Add(time.Hour)}), http.StatusCreated)
	wantStatus(t, "block", f.bob.put("/api/me/blocks/"+f.alice.user.ID.String(), nil), http.StatusNoContent)
	refused := f.alice.dmPost(dm.ID, "still there?")
	wantStatus(t, "posting in the conversation is refused (control)", refused, http.StatusForbidden)
	runCells(t, []doorCell{
		{"blocked member posts", func() response { return f.alice.post(chat, map[string]any{"content": "new words"}) }, http.StatusForbidden},
		{"blocked member edits", func() response {
			return f.alice.put(chat+"/"+mine.String(), map[string]any{"content": "edited after the block"})
		}, http.StatusForbidden},
		{"blocker posts", func() response { return f.bob.post(chat, map[string]any{"content": "to the member I blocked"}) }, http.StatusForbidden},
		{"blocker edits", func() response { return f.bob.put(chat+"/"+theirs.String(), map[string]any{"content": "edited"}) }, http.StatusForbidden},
		{"blocked member reacts", func() response {
			return f.alice.post("/api/messages/"+theirs.String()+"/reactions", map[string]string{"emoji": "👍"})
		}, http.StatusForbidden},
		{"blocked member votes", func() response {
			return f.alice.put(chat+"/"+poll.String()+"/poll/votes", map[string]any{"option_ids": []uuid.UUID{option}})
		}, http.StatusForbidden},
		{"blocked member uploads", func() response {
			return f.alice.upload("/api/channels/"+talk.String()+"/upload", "file", "a.png", "image/png", pngBytes, map[string]string{"content": "look"})
		}, http.StatusForbidden},
		{"blocked member replies in a thread", func() response {
			return f.alice.post(chat, map[string]any{"content": "a reply", "parent_id": theirs})
		}, http.StatusForbidden},
		{"blocked member forwards into the Talk's chat", func() response {
			return f.alice.post("/api/messages/"+public.String()+"/forward", map[string]any{"channel_id": talk})
		}, http.StatusForbidden},
	})
	edit := f.alice.put(chat+"/"+mine.String(), map[string]any{"content": "edited after the block"})
	if got, want := edit.errorBody(t).Error, refused.errorBody(t).Error; got != want {
		t.Errorf("the Talk's chat answers %+v, the conversation %+v", got, want)
	}
	if s := f.bob.readState(t, talk); s.UnreadCount != 0 {
		t.Errorf("the blocker's Talk shows %d unread from the member they blocked", s.UnreadCount)
	}
	fireScheduled(t, f.a)
	var list []struct{ Content string }
	f.bob.get(chat).decode(t, &list)
	for _, m := range list {
		if m.Content == "later words" || m.Content == "edited after the block" {
			t.Errorf("the blocker reads %q in the Talk's chat", m.Content)
		}
	}
}

// Taking back stays open in the Talk's chat as in the conversation: deleting
// one's own message, removing one's reaction and withdrawing one's vote.
func TestABlockLeavesTakingBackOpenInTheTalkChat(t *testing.T) {
	f := newDMFixture(t, true)
	dm := f.alice.mustDM(t, f.bob)
	msgID(t, f.alice.dmPost(dm.ID, "hello bob"))
	msgID(t, f.bob.dmPost(dm.ID, "hi alice"))
	talk := startTalk(t, f.alice, dm.ID)
	chat := "/api/channels/" + talk.String() + "/messages"
	mine := msgID(t, f.alice.post(chat, map[string]any{"content": "in the call chat"}))
	theirs := msgID(t, f.bob.post(chat, map[string]any{"content": "me too"}))
	poll, option := dmPoll(t, f.bob, talk)
	react := func() response {
		return f.alice.post("/api/messages/"+theirs.String()+"/reactions", map[string]string{"emoji": "👍"})
	}
	wantStatus(t, "react", react(), http.StatusOK)
	wantStatus(t, "vote", f.alice.put(chat+"/"+poll.String()+"/poll/votes", map[string]any{"option_ids": []uuid.UUID{option}}), http.StatusOK)
	wantStatus(t, "block", f.bob.put("/api/me/blocks/"+f.alice.user.ID.String(), nil), http.StatusNoContent)
	runCells(t, []doorCell{
		{"blocked member removes a reaction", react, http.StatusOK},
		{"blocked member withdraws a vote", func() response {
			return f.alice.put(chat+"/"+poll.String()+"/poll/votes", map[string]any{"option_ids": []uuid.UUID{}})
		}, http.StatusOK},
		{"blocked member deletes an own message", func() response { return f.alice.delete(chat + "/" + mine.String()) }, http.StatusNoContent},
		{"blocker deletes an own message", func() response { return f.bob.delete(chat + "/" + theirs.String()) }, http.StatusNoContent},
	})
}

func startTalk(t *testing.T, c *client, dm uuid.UUID) uuid.UUID {
	t.Helper()
	res := c.post("/api/dms/"+dm.String()+"/talk", nil)
	wantStatus(t, "start a Talk", res, http.StatusCreated)
	var talk talks.StartTalkResponse
	res.decode(t, &talk)
	return talk.ChannelID
}

// fireScheduled makes every pending scheduled message due and runs the sweep.
func fireScheduled(t *testing.T, a *app) {
	t.Helper()
	ctx := context.Background()
	if _, err := a.db.Exec(ctx, `UPDATE scheduled_messages SET send_at = NOW() - INTERVAL '1 second' WHERE state = 'pending'`); err != nil {
		t.Fatal(err)
	}
	a.router.Later.Sweep(ctx)
}
