//go:build integration

package chat

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/chat/chattest"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/chat/forum"
)

func auditCount(t *testing.T, f chatFixture, action string) int {
	t.Helper()
	var n int
	if err := f.p.QueryRow(context.Background(), `SELECT COUNT(*) FROM audit_log WHERE action = $1`, action).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestForumModerationIsAudited(t *testing.T) {
	f, h, mod := setupForumEnv(t)
	h.Audit = &audit.Log{DB: f.p}
	ch := createTestForum(t, f)
	chParam := map[string]string{"channelID": ch.ID.String()}

	rec := httptest.NewRecorder()
	if err := h.forum().ServeCreateForumTag(rec, chattest.Request(mod, `{"name":"bugs"}`, chParam, "")); err != nil {
		t.Fatal(err)
	}
	tag := forumDecode[core.ForumTag](t, rec)
	tagParam := map[string]string{"channelID": ch.ID.String(), "tagID": tag.ID.String()}
	if err := h.forum().ServeUpdateForumTag(httptest.NewRecorder(), chattest.Request(mod, `{"name":"defects"}`, tagParam, "")); err != nil {
		t.Fatal(err)
	}
	var details string
	if err := f.p.QueryRow(context.Background(), `SELECT details::text FROM audit_log WHERE action = 'forum.tag_update'`).Scan(&details); err != nil {
		t.Fatal(err)
	}
	var d map[string]any
	if err := json.Unmarshal([]byte(details), &d); err != nil || d["previous_name"] != "bugs" || d["name"] != "defects" {
		t.Fatalf("update details = %s, %v", details, err)
	}
	if err := h.forum().ServeDeleteForumTag(httptest.NewRecorder(), chattest.Request(mod, "", tagParam, "")); err != nil {
		t.Fatal(err)
	}

	post := forumMakePost(t, h, f.u, ch, "Mine")
	postParam := map[string]string{"channelID": ch.ID.String(), "messageID": post.ID.String()}
	// The author's own edit is not a moderation action.
	if err := h.forum().ServeUpdateForumPost(httptest.NewRecorder(), chattest.Request(f.u, `{"title":"Mine v2"}`, postParam, "")); err != nil {
		t.Fatal(err)
	}
	if got := auditCount(t, f, "forum.post_update"); got != 0 {
		t.Fatalf("author edit audited %d times", got)
	}
	if err := h.forum().ServeUpdateForumPost(httptest.NewRecorder(), chattest.Request(mod, `{"is_locked":true}`, postParam, "")); err != nil {
		t.Fatal(err)
	}
	for action, want := range map[string]int{"forum.tag_create": 1, "forum.tag_update": 1, "forum.tag_delete": 1, "forum.post_update": 1} {
		if got := auditCount(t, f, action); got != want {
			t.Errorf("%s audited %d times, want %d", action, got, want)
		}
	}
}

func TestForumPostPatchChecksTheLockUnderTheRowLock(t *testing.T) {
	f, h, _ := setupForumEnv(t)
	ch := createTestForum(t, f)
	post := forumMakePost(t, h, f.u, ch, "Mine")
	ctx := context.Background()

	empty := httptest.NewRecorder()
	err := h.forum().ServeUpdateForumPost(empty, chattest.Request(f.u, `{}`, map[string]string{"channelID": ch.ID.String(), "messageID": post.ID.String()}, ""))
	requireAPIStatus(t, err, http.StatusBadRequest)

	tx, err := f.p.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := tx.Exec(ctx, `UPDATE messages SET is_locked = TRUE WHERE id = $1`, post.ID); err != nil {
		t.Fatal(err)
	}
	title := "renamed while locked"
	done := make(chan error, 1)
	go func() {
		_, err := forum.UpdateForumPost(ctx, f.p, ch.ID, post.ID, forum.PostActor{ID: f.u.ID}, forum.PostPatch{Title: &title})
		done <- err
	}()
	waitForBlockedStatement(t, f.p) // the author's request saw the post unlocked
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	requireAPIStatus(t, <-done, http.StatusForbidden)
	var stored string
	if err := f.p.QueryRow(ctx, `SELECT title FROM messages WHERE id = $1`, post.ID).Scan(&stored); err != nil || stored != "Mine" {
		t.Fatalf("title = %q, %v; want unchanged", stored, err)
	}
}

func TestForumActivityPagingUsesTheSeenCursorActivity(t *testing.T) {
	f, h, _ := setupForumEnv(t)
	ch := createTestForum(t, f)
	ctx := context.Background()
	older := forumMakePost(t, h, f.u, ch, "older")
	time.Sleep(10 * time.Millisecond)
	middle := forumMakePost(t, h, f.u, ch, "middle")
	time.Sleep(10 * time.Millisecond)
	newest := forumMakePost(t, h, f.u, ch, "newest")

	page1, err := forum.ListForumPostsFiltered(ctx, f.p, ch.ID, forum.PostFilter{}, core.HistoryQuery{Limit: 2})
	if err != nil || len(page1) != 2 || page1[0].ID != newest.ID || page1[1].ID != middle.ID {
		t.Fatalf("page 1 = %v, %v", page1, err)
	}
	cursor := page1[1]
	// A reply to the cursor post moves its activity past the posts of page 1.
	if err := forumReply(t, h, f.u, ch, middle.ID); err != nil {
		t.Fatal(err)
	}
	page2, err := forum.ListForumPostsFiltered(ctx, f.p, ch.ID, forum.PostFilter{}, core.HistoryQuery{Limit: 2, Before: &cursor.ID, BeforeActivity: cursor.LastActivityAt})
	if err != nil || len(page2) != 1 || page2[0].ID != older.ID {
		t.Fatalf("page 2 = %v, %v; want only the older post", page2, err)
	}
}

func TestForumPostListRejectsUnknownSort(t *testing.T) {
	f, h, _, ch := forumCtx(t)
	err := h.forum().ServeListForumPosts(httptest.NewRecorder(), chattest.Request(f.u, "", map[string]string{"channelID": ch.ID.String()}, url.Values{"sort": {"oldest"}}.Encode()))
	requireAPIStatus(t, err, http.StatusBadRequest)
}
