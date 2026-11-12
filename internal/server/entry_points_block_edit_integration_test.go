//go:build integration

package server

import (
	"net/http"
	"testing"
)

// After a block, both members of a 1:1 conversation still rewrite their
// earlier messages there, and the other side reads the new text (also in the
// "replied to" line above a reply, which the web app does not fold), although
// a block makes the conversation unwritable in both directions
// (doc/architecture.md "Starting and writing"). Finding H-L3-S1-01 of the
// 2026-11 hunt, standing at S3. knownBlockedEdit asserts the WRONG behaviour
// (an inverse assertion): it fails the day the edit is refused; then make the
// two edits cells of TestABlockClosesTheDoorsOfAConversation with 403.
func TestABlockLeavesEditsOpenInTheConversation(t *testing.T) {
	f := newDMFixture(t, false)
	dm := f.alice.mustDM(t, f.bob)
	mine := msgID(t, f.alice.dmPost(dm.ID, "hello bob"))
	theirs := msgID(t, f.bob.dmPost(dm.ID, "hi alice"))
	wantStatus(t, "block", f.bob.put("/api/me/blocks/"+f.alice.user.ID.String(), nil), http.StatusNoContent)
	wantStatus(t, "posting is refused (control)", f.alice.dmPost(dm.ID, "still there?"), http.StatusForbidden)
	base := "/api/channels/" + dm.ID.String() + "/messages/"
	knownBlockedEdit(t, f.alice, base+mine.String(), "the blocked member")
	knownBlockedEdit(t, f.bob, base+theirs.String(), "the blocker")
	var list []struct{ ID, Content string }
	f.bob.get("/api/channels/"+dm.ID.String()+"/messages").decode(t, &list)
	for _, m := range list {
		if m.ID == mine.String() && m.Content != "new words after the block" {
			t.Errorf("the blocker reads %q, the edit did not land", m.Content)
		}
	}
}

func knownBlockedEdit(t *testing.T, c *client, path, who string) {
	t.Helper()
	if res := c.put(path, map[string]any{"content": "new words after the block"}); res.status != http.StatusOK {
		t.Errorf("%s's edit after the block: %d, H-L3-S1-01 is fixed: make it a 403 cell", who, res.status)
	}
}
