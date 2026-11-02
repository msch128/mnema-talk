//go:build integration

package server

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/msch128/mnema-talk/internal/communityevents"
)

// Comments through the real router: a member's comments go with their account.
func TestAnErasedMembersCommentsAreRemoved(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	alice := a.register(admin, "alice")
	ev := a.coverEventAt(admin, time.Now().Add(48*time.Hour), nil)
	path := "/api/events/" + ev.ID.String() + "/comments"

	wantStatus(t, "alice comments", alice.post(path, map[string]string{"content": "I bring snacks"}), http.StatusCreated)
	wantStatus(t, "admin comments", admin.post(path, map[string]string{"content": "Great"}), http.StatusCreated)
	var list communityevents.CommentList
	res := admin.get(path)
	wantStatus(t, "list", res, http.StatusOK)
	res.decode(t, &list)
	if len(list.Comments) != 2 {
		t.Fatalf("comments: %+v", list.Comments)
	}

	wantStatus(t, "alice deletes her account", a.deleteAccount(alice, "member-password-123", ""), http.StatusNoContent)
	res = admin.get(path)
	res.decode(t, &list)
	if len(list.Comments) != 1 || list.Comments[0].Content != "Great" {
		t.Fatalf("only the other member's comment is left: %+v", list.Comments)
	}
	var n int
	if err := a.db.QueryRow(context.Background(), `SELECT COUNT(*) FROM event_comments WHERE event_id = $1`, ev.ID).Scan(&n); err != nil || n != 1 {
		t.Fatalf("rows: %d %v", n, err)
	}
}
