//go:build integration

package chat

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/httpx"
)

func requireAPIStatus(t *testing.T, err error, status int) {
	t.Helper()
	var apiErr *httpx.APIError
	if !errors.As(err, &apiErr) || apiErr.Status != status {
		t.Fatalf("error = %v; want API status %d", err, status)
	}
}

func TestForumTagNameConflictIsReported(t *testing.T) {
	f, _, _ := setupForumEnv(t)
	ch := createTestForum(t, f)
	ctx := context.Background()
	if _, err := CreateForumTag(ctx, f.p, ch.ID, "Bug", ""); err != nil {
		t.Fatal(err)
	}
	_, err := CreateForumTag(ctx, f.p, ch.ID, "Bug", "")
	requireAPIStatus(t, err, http.StatusConflict)
	other, err := CreateForumTag(ctx, f.p, ch.ID, "Question", "")
	if err != nil {
		t.Fatal(err)
	}
	name := "Bug"
	_, _, err = UpdateForumTag(ctx, f.p, ch.ID, other.ID, &name, nil)
	requireAPIStatus(t, err, http.StatusConflict)
}

func TestForumTagUpdatesDoNotOverwriteEachOther(t *testing.T) {
	f, _, _ := setupForumEnv(t)
	ch := createTestForum(t, f)
	ctx := context.Background()
	tag, err := CreateForumTag(ctx, f.p, ch.ID, "old", "A")
	if err != nil {
		t.Fatal(err)
	}
	tx, err := f.p.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	name, emoji := "new", "B"
	if _, _, err := UpdateForumTag(ctx, tx, ch.ID, tag.ID, &name, nil); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, _, err := UpdateForumTag(ctx, f.p, ch.ID, tag.ID, nil, &emoji)
		done <- err
	}()
	time.Sleep(200 * time.Millisecond) // the second patch has read the tag and waits for the row
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	tags, err := ListForumTags(ctx, f.p, ch.ID)
	if err != nil || len(tags) != 1 || tags[0].Name != "new" || tags[0].Emoji != "B" {
		t.Fatalf("tags = %+v, %v; want name new and emoji B", tags, err)
	}
}

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
	if err := h.createForumTag(rec, chatRequest(mod, `{"name":"bugs"}`, chParam, "")); err != nil {
		t.Fatal(err)
	}
	tag := forumDecode[ForumTag](t, rec)
	tagParam := map[string]string{"channelID": ch.ID.String(), "tagID": tag.ID.String()}
	if err := h.updateForumTag(httptest.NewRecorder(), chatRequest(mod, `{"name":"defects"}`, tagParam, "")); err != nil {
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
	if err := h.deleteForumTag(httptest.NewRecorder(), chatRequest(mod, "", tagParam, "")); err != nil {
		t.Fatal(err)
	}

	post := forumMakePost(t, h, f.u, ch, "Mine")
	postParam := map[string]string{"channelID": ch.ID.String(), "messageID": post.ID.String()}
	// The author's own edit is not a moderation action.
	if err := h.updateForumPost(httptest.NewRecorder(), chatRequest(f.u, `{"title":"Mine v2"}`, postParam, "")); err != nil {
		t.Fatal(err)
	}
	if got := auditCount(t, f, "forum.post_update"); got != 0 {
		t.Fatalf("author edit audited %d times", got)
	}
	if err := h.updateForumPost(httptest.NewRecorder(), chatRequest(mod, `{"is_locked":true}`, postParam, "")); err != nil {
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
	err := h.updateForumPost(empty, chatRequest(f.u, `{}`, map[string]string{"channelID": ch.ID.String(), "messageID": post.ID.String()}, ""))
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
		_, err := UpdateForumPost(ctx, f.p, ch.ID, post.ID, f.u.ID, false, &title, nil, nil)
		done <- err
	}()
	time.Sleep(200 * time.Millisecond) // the author's request saw the post unlocked
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

	page1, err := ListForumPosts(ctx, f.p, ch.ID, "", nil, HistoryQuery{Limit: 2})
	if err != nil || len(page1) != 2 || page1[0].ID != newest.ID || page1[1].ID != middle.ID {
		t.Fatalf("page 1 = %v, %v", page1, err)
	}
	cursor := page1[1]
	// A reply to the cursor post moves its activity past the posts of page 1.
	if err := forumReply(t, h, f.u, ch, middle.ID); err != nil {
		t.Fatal(err)
	}
	page2, err := ListForumPosts(ctx, f.p, ch.ID, "", nil, HistoryQuery{Limit: 2, Before: &cursor.ID, BeforeActivity: cursor.LastActivityAt})
	if err != nil || len(page2) != 1 || page2[0].ID != older.ID {
		t.Fatalf("page 2 = %v, %v; want only the older post", page2, err)
	}
}

func TestForumPostListRejectsUnknownSort(t *testing.T) {
	f, h, _, ch := forumCtx(t)
	err := h.listForumPosts(httptest.NewRecorder(), chatRequest(f.u, "", map[string]string{"channelID": ch.ID.String()}, url.Values{"sort": {"oldest"}}.Encode()))
	requireAPIStatus(t, err, http.StatusBadRequest)
}

var errRollBack = errors.New("roll back")

func TestBulkDeleteSurvivesAFailingEditVersionRead(t *testing.T) {
	f := newChatFixture(t)
	ctx := context.Background()
	err := pgx.BeginFunc(ctx, f.p, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `ALTER TABLE message_edits RENAME TO message_edits_gone`); err != nil {
			return err
		}
		gone, _, err := takeLatestMessages(ctx, tx, f.c.ID, f.u.ID, 5, true)
		if err != nil || len(gone) == 0 {
			t.Fatalf("takeLatestMessages = %d messages, %v", len(gone), err)
		}
		return errRollBack // the rename must not outlive the test
	})
	if !errors.Is(err, errRollBack) {
		t.Fatal(err)
	}
}

func TestAnchorLookupFailureIsNotAMissingMessage(t *testing.T) {
	f := newChatFixture(t)
	h := &Handler{DB: f.p}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	q := url.Values{"before": {f.m.String()}}.Encode()
	r := chatRequest(f.u, "", nil, q).WithContext(ctx)
	if _, err := h.parseChannelMessageQuery(r, f.c.ID); err == nil || errors.Is(err, errMessageNotFound) {
		t.Fatalf("channel anchor error = %v; want the lookup failure", err)
	}
	if _, err := h.parseThreadQuery(r, f.m); err == nil || errors.Is(err, errMessageNotFound) {
		t.Fatalf("thread anchor error = %v; want the lookup failure", err)
	}
	live := chatRequest(f.u, "", nil, url.Values{"before": {uuid.NewString()}}.Encode())
	if _, err := h.parseChannelMessageQuery(live, f.c.ID); !errors.Is(err, errMessageNotFound) {
		t.Fatalf("unknown anchor error = %v; want not found", err)
	}
}

func TestChannelEditsAreAllOrNothing(t *testing.T) {
	f := newChatFixture(t)
	ctx := context.Background()
	if _, err := CreateChannelWithStage(ctx, f.p, nil, "text-stage", ChannelTypeText, "", 0, true); err == nil {
		t.Fatal("a text channel was created as a Stage")
	}
	var n int
	if err := f.p.QueryRow(ctx, `SELECT COUNT(*) FROM channels WHERE name = 'text-stage'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("channels named text-stage = %d, %v; want none", n, err)
	}

	voice, err := CreateChannel(ctx, f.p, nil, "talk", ChannelTypeVoice, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	stage, bad := true, -1
	if _, err := EditChannel(ctx, f.p, voice.ID, nil, nil, &bad, nil, nil, &stage); err == nil {
		t.Fatal("invalid limit accepted")
	}
	if _, err := EditChannel(ctx, f.p, f.c.ID, nil, nil, nil, nil, nil, &stage); err == nil {
		t.Fatal("a text channel became a Stage")
	}
	var isStage bool
	if err := f.p.QueryRow(ctx, `SELECT is_stage FROM channels WHERE id = $1`, voice.ID).Scan(&isStage); err != nil || isStage {
		t.Fatalf("is_stage = %v, %v; want unchanged", isStage, err)
	}
}

func TestRenameCategoryReturnsTheStoredName(t *testing.T) {
	f := newChatFixture(t)
	ctx := context.Background()
	var id uuid.UUID
	if err := f.p.QueryRow(ctx, `INSERT INTO categories (name) VALUES ('x') RETURNING id`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	got, err := RenameCategory(ctx, f.p, id, "  Games  ")
	if err != nil || got != "Games" {
		t.Fatalf("RenameCategory = %q, %v; want Games", got, err)
	}
}
