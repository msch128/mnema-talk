//go:build integration

package server

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// A forum search the database cannot finish in time answers like a message
// search that is too slow, and the forum keeps working afterwards.
func TestForumSearchOverTheStatementLimitAnswersLikeASlowSearch(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	forum := a.createChannel(admin, "ideas", "forum")
	newForumPost(t, admin, forum, "Bicycle parking")
	search := "/api/channels/" + forum.String() + "/posts?q=parking"
	wantStatus(t, "search before the lock", admin.get(search), http.StatusOK)

	// An exclusive lock on the table makes every read of it wait, which is
	// what a search over a huge forum looks like to the statement timeout.
	ctx := context.Background()
	tx, err := a.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `LOCK TABLE messages IN ACCESS EXCLUSIVE MODE`); err != nil {
		t.Fatal(err)
	}

	res := admin.get(search)
	wantStatus(t, "search behind the lock", res, http.StatusTooManyRequests)
	var body struct {
		Error struct{ Code, Message string }
	}
	res.decode(t, &body)
	if body.Error.Code != "RATE_LIMITED" || !strings.Contains(body.Error.Message, "the search took too long") {
		t.Fatalf("slow forum search answered %s; want RATE_LIMITED and the slow-search text", res.body)
	}

	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	wantStatus(t, "search after the lock", admin.get(search), http.StatusOK)
}
