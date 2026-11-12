//go:build integration

package server

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// A refused edit in a conversation of two answers exactly like a refused new
// message (doc/architecture.md "Starting and writing": the same 403 text,
// whatever the reason), so a client shows the same words and nothing tells
// the blocked member more than a refused post does.
func TestARefusedEditAfterABlockAnswersLikeARefusedPost(t *testing.T) {
	f := newDMFixture(t, false)
	dm := f.alice.mustDM(t, f.bob)
	mine := msgID(t, f.alice.dmPost(dm.ID, "hello bob"))
	msgID(t, f.bob.dmPost(dm.ID, "hi alice"))
	wantStatus(t, "block", f.bob.put("/api/me/blocks/"+f.alice.user.ID.String(), nil), http.StatusNoContent)
	post := f.alice.dmPost(dm.ID, "still there?")
	edit := f.alice.put("/api/channels/"+dm.ID.String()+"/messages/"+mine.String(), map[string]any{"content": "new words"})
	if post.status != http.StatusForbidden || edit.status != post.status {
		t.Fatalf("post %d, edit %d %s: want both 403", post.status, edit.status, edit.body)
	}
	if p, e := post.errorBody(t).Error, edit.errorBody(t).Error; p != e {
		t.Errorf("edit answers %+v, a refused post %+v", e, p)
	}
}

// What a block leaves open in a conversation of two: taking things away is
// not writing to anybody. Each side may still delete their own messages and
// withdraw their votes, and a group conversation goes on as before (a block
// does not stop a group, doc/moderation.md "Blocking a member").
func TestABlockLeavesTakingBackOpen(t *testing.T) {
	f := newDMFixture(t, false)
	dm := f.alice.mustDM(t, f.bob)
	mine := msgID(t, f.alice.dmPost(dm.ID, "hello bob"))
	poll, option := dmPoll(t, f.bob, dm.ID)
	wantStatus(t, "vote", f.alice.put("/api/channels/"+dm.ID.String()+"/messages/"+poll.String()+"/poll/votes",
		map[string]any{"option_ids": []uuid.UUID{option}}), http.StatusOK)
	group := f.carol.mustDM(t, f.alice, f.bob)
	inGroup := msgID(t, f.alice.dmPost(group.ID, "hello all"))
	wantStatus(t, "block", f.bob.put("/api/me/blocks/"+f.alice.user.ID.String(), nil), http.StatusNoContent)
	base := "/api/channels/" + dm.ID.String() + "/messages/"
	runCells(t, []doorCell{
		{"blocked member withdraws a vote", func() response {
			return f.alice.put(base+poll.String()+"/poll/votes", map[string]any{"option_ids": []uuid.UUID{}})
		}, http.StatusOK},
		{"blocked member deletes an own message", func() response { return f.alice.delete(base + mine.String()) }, http.StatusNoContent},
		{"blocker deletes an own message", func() response { return f.bob.delete(base + poll.String()) }, http.StatusNoContent},
		{"blocked member edits in a group", func() response {
			return f.alice.put("/api/channels/"+group.ID.String()+"/messages/"+inGroup.String(), map[string]any{"content": "edited"})
		}, http.StatusOK},
	})
}

// A conversation of two whose other side was disabled takes no new message,
// and no edit either (the same rule of checkDMReachable).
func TestNoEditReachesADisabledAccountInAConversationOfTwo(t *testing.T) {
	f := newDMFixture(t, false)
	dm := f.alice.mustDM(t, f.bob)
	mine := msgID(t, f.alice.dmPost(dm.ID, "hello bob"))
	wantStatus(t, "disable", f.admin.post("/api/admin/users/"+f.bob.user.ID.String()+"/disable", nil), http.StatusNoContent)
	wantStatus(t, "post (control)", f.alice.dmPost(dm.ID, "are you there?"), http.StatusForbidden)
	wantStatus(t, "edit", f.alice.put("/api/channels/"+dm.ID.String()+"/messages/"+mine.String(), map[string]any{"content": "new words"}), http.StatusForbidden)
}
