//go:build integration

package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/auth"
)

// forumTagFixture is a forum with a plain and a moderated tag, a regular member
// (f.u) and the Owner (mod).
type forumTagFixture struct {
	f         chatFixture
	h         *Handler
	mod       *auth.User
	ch        *Channel
	chParam   map[string]string
	plain     *ForumTag
	moderated *ForumTag
}

func newForumTagFixture(t *testing.T) forumTagFixture {
	t.Helper()
	f, h, mod := setupForumEnv(t)
	ch := createTestForum(t, f)
	ctx := context.Background()
	plain, err := CreateForumTagSpec(ctx, f.p, ch.ID, TagSpec{Name: "help", Color: "blue"})
	if err != nil {
		t.Fatal(err)
	}
	moderated, err := CreateForumTagSpec(ctx, f.p, ch.ID, TagSpec{Name: "official", Moderated: true})
	if err != nil {
		t.Fatal(err)
	}
	return forumTagFixture{f, h, mod, ch, map[string]string{"channelID": ch.ID.String()}, plain, moderated}
}

func (x forumTagFixture) createPost(t *testing.T, u *auth.User, title string, tagIDs ...uuid.UUID) (*Message, error) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"title": title, "content": "body of " + title, "tag_ids": tagIDs})
	rec := httptest.NewRecorder()
	if err := x.h.createForumPost(rec, chatRequest(u, string(body), x.chParam, "")); err != nil {
		return nil, err
	}
	var m Message
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	return &m, nil
}

func (x forumTagFixture) patchPost(u *auth.User, post *Message, body string) error {
	params := map[string]string{"channelID": x.ch.ID.String(), "messageID": post.ID.String()}
	return x.h.updateForumPost(httptest.NewRecorder(), chatRequest(u, body, params, ""))
}

func (x forumTagFixture) list(t *testing.T, u *auth.User, query string) []Message {
	t.Helper()
	rec := httptest.NewRecorder()
	if err := x.h.listForumPosts(rec, chatRequest(u, "", x.chParam, query)); err != nil {
		t.Fatal(err)
	}
	var out []Message
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestForumTagColorOrderAndModeratedFlag(t *testing.T) {
	x := newForumTagFixture(t)
	ctx := context.Background()

	if x.plain.Color != "blue" || x.plain.Position != 1 || x.moderated.Position != 2 || !x.moderated.Moderated {
		t.Fatalf("created tags: %+v %+v", x.plain, x.moderated)
	}
	if _, err := CreateForumTagSpec(ctx, x.f.p, x.ch.ID, TagSpec{Name: "bad", Color: "#ff0000"}); err == nil {
		t.Fatal("a raw CSS colour must be refused")
	}

	// Colour and flag change on their own; an empty colour clears it.
	green, off, empty := "green", false, ""
	got, before, err := UpdateForumTag(ctx, x.f.p, x.ch.ID, x.moderated.ID, TagPatch{Color: &green, Moderated: &off})
	if err != nil || got.Color != "green" || got.Moderated || !before.Moderated || before.Color != "" {
		t.Fatalf("update: %+v before %+v err %v", got, before, err)
	}
	if got, _, err = UpdateForumTag(ctx, x.f.p, x.ch.ID, x.plain.ID, TagPatch{Color: &empty}); err != nil || got.Color != "" || got.Name != "help" {
		t.Fatalf("clear colour: %+v %v", got, err)
	}

	// Reorder: a regular member may not, the list follows the new order, a partial list is refused.
	order := fmt.Sprintf(`{"tag_ids":["%s","%s"]}`, x.moderated.ID, x.plain.ID)
	if err := x.h.reorderForumTags(httptest.NewRecorder(), chatRequest(x.f.u, order, x.chParam, "")); err == nil {
		t.Fatal("a member without Manage channels must not reorder tags")
	}
	if err := x.h.reorderForumTags(httptest.NewRecorder(), chatRequest(x.mod, order, x.chParam, "")); err != nil {
		t.Fatalf("reorder: %v", err)
	}
	tags, err := ListForumTags(ctx, x.f.p, x.ch.ID)
	if err != nil || len(tags) != 2 || tags[0].ID != x.moderated.ID {
		t.Fatalf("order after reorder: %+v %v", tags, err)
	}
	partial := fmt.Sprintf(`{"tag_ids":["%s"]}`, x.plain.ID)
	if err := x.h.reorderForumTags(httptest.NewRecorder(), chatRequest(x.mod, partial, x.chParam, "")); err == nil {
		t.Fatal("an order that misses a tag must be refused")
	}
	dup := fmt.Sprintf(`{"tag_ids":["%s","%s"]}`, x.plain.ID, x.plain.ID)
	if err := x.h.reorderForumTags(httptest.NewRecorder(), chatRequest(x.mod, dup, x.chParam, "")); err == nil {
		t.Fatal("an order that repeats a tag must be refused")
	}

	// A tag created later goes last.
	last, err := CreateForumTag(ctx, x.f.p, x.ch.ID, "later", "")
	if err != nil || last.Position != 3 {
		t.Fatalf("new tag position: %+v %v", last, err)
	}
}

func TestForumModeratedTagsNeedManageMessages(t *testing.T) {
	x := newForumTagFixture(t)

	if _, err := x.createPost(t, x.f.u, "member with official tag", x.moderated.ID); err == nil {
		t.Fatal("a member must not create a post with a moderated tag")
	}
	post, err := x.createPost(t, x.f.u, "member post", x.plain.ID)
	if err != nil {
		t.Fatal(err)
	}

	// The author edits the plain tags freely but cannot add or remove a moderated one.
	both := fmt.Sprintf(`{"tag_ids":["%s","%s"]}`, x.plain.ID, x.moderated.ID)
	if err := x.patchPost(x.f.u, post, both); err == nil {
		t.Fatal("author added a moderated tag")
	}
	if err := x.patchPost(x.mod, post, both); err != nil {
		t.Fatalf("moderator applies moderated tag: %v", err)
	}
	if err := x.patchPost(x.f.u, post, fmt.Sprintf(`{"tag_ids":["%s"]}`, x.plain.ID)); err == nil {
		t.Fatal("author removed a moderated tag")
	}
	// Keeping the moderated tag while changing a plain one is fine.
	other, err := CreateForumTag(context.Background(), x.f.p, x.ch.ID, "more", "")
	if err != nil {
		t.Fatal(err)
	}
	keep := fmt.Sprintf(`{"tag_ids":["%s","%s"]}`, other.ID, x.moderated.ID)
	if err := x.patchPost(x.f.u, post, keep); err != nil {
		t.Fatalf("author changes a plain tag next to a moderated one: %v", err)
	}
	got := x.list(t, x.f.u, "")
	if len(got) != 1 || len(got[0].Tags) != 2 {
		t.Fatalf("tags after edits: %+v", got)
	}
}

func TestForumRequiredTag(t *testing.T) {
	x := newForumTagFixture(t)
	ctx := context.Background()
	settingsParam := x.chParam

	if err := x.h.updateForumSettings(httptest.NewRecorder(), chatRequest(x.f.u, `{"require_tag":true}`, settingsParam, "")); err == nil {
		t.Fatal("a member must not change the forum settings")
	}
	rec := httptest.NewRecorder()
	if err := x.h.updateForumSettings(rec, chatRequest(x.mod, `{"require_tag":true,"guidelines":"Search first."}`, settingsParam, "")); err != nil {
		t.Fatal(err)
	}
	got, err := GetForumSettings(ctx, x.f.p, x.ch.ID)
	if err != nil || !got.RequireTag || got.Guidelines != "Search first." {
		t.Fatalf("settings: %+v %v", got, err)
	}

	if _, err := x.createPost(t, x.f.u, "no tag"); err == nil {
		t.Fatal("a post without a tag must be refused in a forum that requires one")
	}
	post, err := x.createPost(t, x.f.u, "with tag", x.plain.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := x.patchPost(x.f.u, post, `{"tag_ids":[]}`); err == nil {
		t.Fatal("removing the last tag must be refused")
	}
	if err := x.patchPost(x.f.u, post, `{"title":"renamed"}`); err != nil {
		t.Fatalf("a title edit leaves the tags alone: %v", err)
	}

	// Waived when the member may apply no tag at all: only the moderated one is left.
	if _, err := DeleteForumTag(ctx, x.f.p, x.ch.ID, x.plain.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := x.createPost(t, x.f.u, "nothing usable"); err != nil {
		t.Fatalf("the requirement is waived without usable tags: %v", err)
	}
	if _, err := x.createPost(t, x.mod, "moderator needs one"); err == nil {
		t.Fatal("the moderator can apply the moderated tag, so the requirement holds for them")
	}

	long := make([]byte, MaxForumGuidelinesLen+1)
	for i := range long {
		long[i] = 'a'
	}
	body := fmt.Sprintf(`{"guidelines":%q}`, string(long))
	if err := x.h.updateForumSettings(httptest.NewRecorder(), chatRequest(x.mod, body, settingsParam, "")); err == nil {
		t.Fatal("over-long guidelines must be refused")
	}
}

func TestForumPostTagLimitOnEdit(t *testing.T) {
	x := newForumTagFixture(t)
	ctx := context.Background()
	ids := []uuid.UUID{x.plain.ID}
	for i := 0; i < MaxPostTags; i++ {
		tag, err := CreateForumTag(ctx, x.f.p, x.ch.ID, fmt.Sprintf("t%d", i), "")
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, tag.ID)
	}
	post, err := x.createPost(t, x.f.u, "limit")
	if err != nil {
		t.Fatal(err)
	}
	over, _ := json.Marshal(map[string]any{"tag_ids": ids}) // MaxPostTags + 1
	if err := x.patchPost(x.f.u, post, string(over)); err == nil {
		t.Fatal("more than MaxPostTags tags must be refused on edit")
	}
	ok, _ := json.Marshal(map[string]any{"tag_ids": ids[:MaxPostTags]})
	if err := x.patchPost(x.f.u, post, string(ok)); err != nil {
		t.Fatalf("MaxPostTags tags are allowed: %v", err)
	}
}

func TestForumResolvedPermissionsAndLock(t *testing.T) {
	x := newForumTagFixture(t)
	post, err := x.createPost(t, x.f.u, "question")
	if err != nil {
		t.Fatal(err)
	}
	stranger := &auth.User{Username: "stranger", DisplayName: "Stranger", Role: auth.RoleUser}
	if err := x.f.p.QueryRow(context.Background(), `INSERT INTO users (username, display_name, password_hash) VALUES ($1,$2,'x') RETURNING id, created_at`, stranger.Username, stranger.DisplayName).Scan(&stranger.ID, &stranger.CreatedAt); err != nil {
		t.Fatal(err)
	}
	if err := x.patchPost(stranger, post, `{"is_resolved":true}`); err == nil {
		t.Fatal("a stranger must not mark someone else's post solved")
	}
	if err := x.patchPost(x.f.u, post, `{"is_resolved":true}`); err != nil {
		t.Fatalf("author marks solved: %v", err)
	}
	if got := x.list(t, x.f.u, ""); len(got) != 1 || !got[0].IsResolved {
		t.Fatalf("resolved flag in the list: %+v", got)
	}
	if err := x.patchPost(x.mod, post, `{"is_locked":true}`); err != nil {
		t.Fatal(err)
	}
	if err := x.patchPost(x.f.u, post, `{"is_resolved":false}`); err == nil {
		t.Fatal("the author must not reopen a locked post")
	}
	if err := x.patchPost(x.mod, post, `{"is_resolved":false}`); err != nil {
		t.Fatalf("moderator reopens: %v", err)
	}
}

func TestForumListPinnedSearchAndTagFilter(t *testing.T) {
	x := newForumTagFixture(t)
	ctx := context.Background()
	older, _ := x.createPost(t, x.f.u, "Alpha roles", x.plain.ID)
	_, _ = x.createPost(t, x.f.u, "Beta voice")
	newest, _ := x.createPost(t, x.f.u, "Gamma 100%_done")
	if _, err := x.f.p.Exec(ctx, `UPDATE messages SET is_pinned = TRUE, pinned_at = NOW() WHERE id = $1`, older.ID); err != nil {
		t.Fatal(err)
	}

	// The pinned post leads the first page; the cursor page behind it does not repeat it.
	got := x.list(t, x.f.u, "sort=newest")
	if len(got) != 3 || got[0].ID != older.ID || got[1].ID != newest.ID {
		t.Fatalf("pinned first: %v", titles(got))
	}
	next := x.list(t, x.f.u, "sort=newest&limit=1&before="+newest.ID.String())
	if len(next) != 1 || next[0].Title == nil || *next[0].Title != "Beta voice" {
		t.Fatalf("page behind the cursor: %v", titles(next))
	}

	// Search matches the title, escapes LIKE wildcards and also looks into replies.
	if got := x.list(t, x.f.u, "q=roles"); len(got) != 1 || got[0].ID != older.ID {
		t.Fatalf("search title: %v", titles(got))
	}
	if got := x.list(t, x.f.u, "q=100%25_"); len(got) != 1 || got[0].ID != newest.ID {
		t.Fatalf("search with wildcards: %v", titles(got))
	}
	if got := x.list(t, x.f.u, "q=%25%25"); len(got) != 0 {
		t.Fatalf("two percent signs are literal text, not a wildcard, got %v", titles(got))
	}
	reply := fmt.Sprintf(`{"content":"the needle is here","parent_id":"%s"}`, newest.ID)
	if err := x.h.createMessage(httptest.NewRecorder(), chatRequest(x.f.u, reply, x.chParam, "")); err != nil {
		t.Fatal(err)
	}
	if got := x.list(t, x.f.u, "q=needle"); len(got) != 1 || got[0].ID != newest.ID {
		t.Fatalf("search in replies: %v", titles(got))
	}
	if got := x.list(t, x.f.u, "q=roles&tag_id="+x.plain.ID.String()); len(got) != 1 {
		t.Fatalf("search and tag filter combine: %v", titles(got))
	}
	if got := x.list(t, x.f.u, "q=voice&tag_id="+x.plain.ID.String()); len(got) != 0 {
		t.Fatalf("tag filter narrows the search: %v", titles(got))
	}
	long := make([]byte, MaxPostSearchLen+1)
	for i := range long {
		long[i] = 'a'
	}
	if err := x.h.listForumPosts(httptest.NewRecorder(), chatRequest(x.f.u, "", x.chParam, "q="+string(long))); err == nil {
		t.Fatal("an over-long search must be refused")
	}
}

func titles(ms []Message) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		if m.Title != nil {
			out[i] = *m.Title
		}
	}
	return out
}

func TestForumUnreadPostsAndMarkRead(t *testing.T) {
	x := newForumTagFixture(t)
	ctx := context.Background()
	post, err := x.createPost(t, x.mod, "from the moderator")
	if err != nil {
		t.Fatal(err)
	}
	mine, err := x.createPost(t, x.f.u, "from the member")
	if err != nil {
		t.Fatal(err)
	}
	// The member joined before the moderator's post.
	if _, err := x.f.p.Exec(ctx, `UPDATE users SET created_at = NOW() - interval '1 hour' WHERE id = $1`, x.f.u.ID); err != nil {
		t.Fatal(err)
	}

	unread, err := UnreadPostIDs(ctx, x.f.p, x.ch.ID, x.f.u.ID)
	if err != nil || len(unread) != 1 || unread[0] != post.ID {
		t.Fatalf("unread before opening: %v %v", unread, err)
	}
	prev, err := MarkPostRead(ctx, x.f.p, x.f.u.ID, post.ID)
	if err != nil || prev.After(post.CreatedAt) {
		t.Fatalf("first open reports the day of joining, got %v (post %v) %v", prev, post.CreatedAt, err)
	}
	if unread, _ = UnreadPostIDs(ctx, x.f.p, x.ch.ID, x.f.u.ID); len(unread) != 0 {
		t.Fatalf("unread after opening: %v", unread)
	}

	// A reply of someone else makes the member's own post unread; their own reply does not.
	reply := fmt.Sprintf(`{"content":"an answer","parent_id":"%s"}`, mine.ID)
	if err := x.h.createMessage(httptest.NewRecorder(), chatRequest(x.mod, reply, x.chParam, "")); err != nil {
		t.Fatal(err)
	}
	own := fmt.Sprintf(`{"content":"my follow-up","parent_id":"%s"}`, post.ID)
	if err := x.h.createMessage(httptest.NewRecorder(), chatRequest(x.f.u, own, x.chParam, "")); err != nil {
		t.Fatal(err)
	}
	if unread, _ = UnreadPostIDs(ctx, x.f.p, x.ch.ID, x.f.u.ID); len(unread) != 1 || unread[0] != mine.ID {
		t.Fatalf("unread after a reply: %v", unread)
	}

	// The handler refuses a reply id (not a post) and answers with the previous marker.
	params := map[string]string{"channelID": x.ch.ID.String(), "messageID": mine.ID.String()}
	rec := httptest.NewRecorder()
	if err := x.h.markForumPostRead(rec, chatRequest(x.f.u, "", params, "")); err != nil {
		t.Fatal(err)
	}
	var out PostRead
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.PreviousReadAt.IsZero() {
		t.Fatalf("read response: %s %v", rec.Body.String(), err)
	}
	var replyID uuid.UUID
	if err := x.f.p.QueryRow(ctx, `SELECT id FROM messages WHERE parent_id = $1`, mine.ID).Scan(&replyID); err != nil {
		t.Fatal(err)
	}
	params["messageID"] = replyID.String()
	if err := x.h.markForumPostRead(httptest.NewRecorder(), chatRequest(x.f.u, "", params, "")); err == nil {
		t.Fatal("a reply is not a post")
	}
}

func TestForumRulesReorderAndSolvedAreAudited(t *testing.T) {
	x := newForumTagFixture(t)
	x.h.Audit = &audit.Log{DB: x.f.p}
	ctx := context.Background()

	order := fmt.Sprintf(`{"tag_ids":["%s","%s"]}`, x.moderated.ID, x.plain.ID)
	if err := x.h.reorderForumTags(httptest.NewRecorder(), chatRequest(x.mod, order, x.chParam, "")); err != nil {
		t.Fatal(err)
	}
	if err := x.h.updateForumSettings(httptest.NewRecorder(), chatRequest(x.mod, `{"require_tag":true,"guidelines":"Be kind."}`, x.chParam, "")); err != nil {
		t.Fatal(err)
	}
	post, err := x.createPost(t, x.f.u, "solved by a moderator", x.plain.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := x.patchPost(x.f.u, post, `{"is_resolved":true}`); err != nil {
		t.Fatal(err)
	}
	if err := x.patchPost(x.mod, post, fmt.Sprintf(`{"is_resolved":false,"tag_ids":["%s"]}`, x.moderated.ID)); err != nil {
		t.Fatal(err)
	}

	for action, want := range map[string]int{"forum.tag_reorder": 1, "forum.settings_update": 1, "forum.post_update": 1} {
		var n int
		if err := x.f.p.QueryRow(ctx, `SELECT COUNT(*) FROM audit_log WHERE action = $1`, action).Scan(&n); err != nil || n != want {
			t.Errorf("%s audited %d times, want %d (%v)", action, n, want, err)
		}
	}
	var details string
	if err := x.f.p.QueryRow(ctx, `SELECT details::text FROM audit_log WHERE action = 'forum.post_update'`).Scan(&details); err != nil {
		t.Fatal(err)
	}
	var d map[string]any
	if err := json.Unmarshal([]byte(details), &d); err != nil || d["is_resolved"] != false || d["previous_tag_ids"] == nil || d["tag_ids"] == nil {
		t.Fatalf("post_update details = %s, %v", details, err)
	}
}
