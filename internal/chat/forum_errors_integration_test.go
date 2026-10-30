//go:build integration

package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/events"
)

type forumHandlerFn func(*Handler, http.ResponseWriter, *http.Request) error

func forumCtx(t *testing.T) (chatFixture, *Handler, *auth.User, *Channel) {
	t.Helper()
	f, h, mod := setupForumEnv(t)
	return f, h, mod, createTestForum(t, f)
}

func forumNewUser(t *testing.T, f chatFixture, name string) *auth.User {
	t.Helper()
	u := &auth.User{Username: name, DisplayName: name, Role: auth.RoleUser}
	err := f.p.QueryRow(context.Background(), `INSERT INTO users (username, display_name, password_hash) VALUES ($1,$2,'x') RETURNING id, created_at`, name, name).Scan(&u.ID, &u.CreatedAt)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func forumDecode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return v
}

func forumMakePost(t *testing.T, h *Handler, u *auth.User, ch *Channel, title string, tags ...uuid.UUID) Message {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"title": title, "content": "body of " + title, "tag_ids": tags})
	rec := httptest.NewRecorder()
	err := h.createForumPost(rec, chatRequest(u, string(body), map[string]string{"channelID": ch.ID.String()}, ""))
	if err != nil {
		t.Fatalf("create post %q: %v", title, err)
	}
	return forumDecode[Message](t, rec)
}

func forumReply(t *testing.T, h *Handler, u *auth.User, ch *Channel, parent uuid.UUID) error {
	t.Helper()
	body := fmt.Sprintf(`{"content":"reply","parent_id":"%s"}`, parent)
	return h.createMessage(httptest.NewRecorder(), chatRequest(u, body, map[string]string{"channelID": ch.ID.String()}, ""))
}

func forumReplyID(t *testing.T, f chatFixture, parent uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := f.p.QueryRow(context.Background(), `SELECT id FROM messages WHERE parent_id = $1`, parent).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func forumListIDs(t *testing.T, h *Handler, u *auth.User, ch *Channel, query string) []uuid.UUID {
	t.Helper()
	rec := httptest.NewRecorder()
	if err := h.listForumPosts(rec, chatRequest(u, "", map[string]string{"channelID": ch.ID.String()}, query)); err != nil {
		t.Fatalf("list %q: %v", query, err)
	}
	ids := []uuid.UUID{}
	for _, m := range forumDecode[[]Message](t, rec) {
		ids = append(ids, m.ID)
	}
	return ids
}

func requireIDs(t *testing.T, label string, got []uuid.UUID, want ...Message) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: got %d posts, want %d", label, len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i].ID {
			t.Fatalf("%s: position %d is %s, want %s", label, i, got[i], want[i].ID)
		}
	}
}

type forumBadCase struct {
	name   string
	fn     forumHandlerFn
	user   *auth.User
	body   string
	params map[string]string
	query  string
	want   string
}

type forumBadEnv struct {
	member, mod        *auth.User
	chID, textID       string
	tagID, msgID, gone string
}

func (e forumBadEnv) ch() map[string]string { return map[string]string{"channelID": e.chID} }

func (e forumBadEnv) tagParams(ch, tag string) map[string]string {
	return map[string]string{"channelID": ch, "tagID": tag}
}

func (e forumBadEnv) postParams(ch, msg string) map[string]string {
	return map[string]string{"channelID": ch, "messageID": msg}
}

func forumBadTagCases(e forumBadEnv) []forumBadCase {
	longName := `{"name":"` + strings.Repeat("n", 21) + `"}`
	longEmoji := `{"name":"x","emoji":"` + strings.Repeat("e", 33) + `"}`
	bad := map[string]string{"channelID": "nope"}
	return []forumBadCase{
		{"list tags bad channel id", (*Handler).listForumTags, e.mod, "", bad, "", ""},
		{"list tags on text channel", (*Handler).listForumTags, e.mod, "", map[string]string{"channelID": e.textID}, "", "not a forum"},
		{"list tags unknown channel", (*Handler).listForumTags, e.mod, "", map[string]string{"channelID": e.gone}, "", ""},
		{"create tag bad channel id", (*Handler).createForumTag, e.mod, `{"name":"x"}`, bad, "", ""},
		{"create tag on text channel", (*Handler).createForumTag, e.mod, `{"name":"x"}`, map[string]string{"channelID": e.textID}, "", "not a forum"},
		{"create tag broken json", (*Handler).createForumTag, e.mod, `{`, e.ch(), "", ""},
		{"create tag blank name", (*Handler).createForumTag, e.mod, `{"name":"   "}`, e.ch(), "", ""},
		{"create tag long name", (*Handler).createForumTag, e.mod, longName, e.ch(), "", ""},
		{"create tag long emoji", (*Handler).createForumTag, e.mod, longEmoji, e.ch(), "", ""},
		{"update tag bad channel id", (*Handler).updateForumTag, e.mod, `{"name":"y"}`, e.tagParams("nope", e.tagID), "", ""},
		{"update tag bad tag id", (*Handler).updateForumTag, e.mod, `{"name":"y"}`, e.tagParams(e.chID, "nope"), "", ""},
		{"update tag by member", (*Handler).updateForumTag, e.member, `{"name":"y"}`, e.tagParams(e.chID, e.tagID), "", ""},
		{"update tag on text channel", (*Handler).updateForumTag, e.mod, `{"name":"y"}`, e.tagParams(e.textID, e.tagID), "", "not a forum"},
		{"update tag broken json", (*Handler).updateForumTag, e.mod, `[`, e.tagParams(e.chID, e.tagID), "", ""},
		{"update tag without fields", (*Handler).updateForumTag, e.mod, `{}`, e.tagParams(e.chID, e.tagID), "", "no fields"},
		{"update unknown tag", (*Handler).updateForumTag, e.mod, `{"name":"y"}`, e.tagParams(e.chID, e.gone), "", "not found"},
		{"update tag blank name", (*Handler).updateForumTag, e.mod, `{"name":" "}`, e.tagParams(e.chID, e.tagID), "", ""},
		{"update tag long emoji", (*Handler).updateForumTag, e.mod, longEmoji, e.tagParams(e.chID, e.tagID), "", ""},
		{"delete tag bad channel id", (*Handler).deleteForumTag, e.mod, "", e.tagParams("nope", e.tagID), "", ""},
		{"delete tag bad tag id", (*Handler).deleteForumTag, e.mod, "", e.tagParams(e.chID, "nope"), "", ""},
		{"delete tag by member", (*Handler).deleteForumTag, e.member, "", e.tagParams(e.chID, e.tagID), "", ""},
		{"delete tag on text channel", (*Handler).deleteForumTag, e.mod, "", e.tagParams(e.textID, e.tagID), "", "not a forum"},
		{"delete unknown tag", (*Handler).deleteForumTag, e.mod, "", e.tagParams(e.chID, e.gone), "", "not found"},
	}
}

func forumBadPostCases(e forumBadEnv) []forumBadCase {
	okBody := `{"title":"t","content":"c"}`
	longTitle := `{"title":"` + strings.Repeat("t", 101) + `","content":"c"}`
	foreignTag := fmt.Sprintf(`{"title":"t","content":"c","tag_ids":["%s"]}`, e.gone)
	foreignTagUpdate := fmt.Sprintf(`{"tag_ids":["%s"]}`, e.gone)
	text := map[string]string{"channelID": e.textID}
	return []forumBadCase{
		{"list posts bad channel id", (*Handler).listForumPosts, e.member, "", map[string]string{"channelID": "nope"}, "", ""},
		{"list posts on text channel", (*Handler).listForumPosts, e.member, "", text, "", "not a forum"},
		{"list posts bad tag filter", (*Handler).listForumPosts, e.member, "", e.ch(), "tag_id=zzz", "tag_id"},
		{"list posts bad cursor", (*Handler).listForumPosts, e.member, "", e.ch(), "before=zzz", "before"},
		{"create post bad channel id", (*Handler).createForumPost, e.member, okBody, map[string]string{"channelID": "nope"}, "", ""},
		{"create post on text channel", (*Handler).createForumPost, e.member, okBody, text, "", "not a forum"},
		{"create post broken json", (*Handler).createForumPost, e.member, `{`, e.ch(), "", ""},
		{"create post long title", (*Handler).createForumPost, e.member, longTitle, e.ch(), "", ""},
		{"create post foreign tag", (*Handler).createForumPost, e.member, foreignTag, e.ch(), "", "tag does not belong"},
		{"update post bad channel id", (*Handler).updateForumPost, e.member, `{"title":"x"}`, e.postParams("nope", e.msgID), "", ""},
		{"update post bad message id", (*Handler).updateForumPost, e.member, `{"title":"x"}`, e.postParams(e.chID, "nope"), "", ""},
		{"update post on text channel", (*Handler).updateForumPost, e.member, `{"title":"x"}`, e.postParams(e.textID, e.msgID), "", "not a forum"},
		{"update post broken json", (*Handler).updateForumPost, e.member, `{`, e.postParams(e.chID, e.msgID), "", ""},
		{"update unknown post", (*Handler).updateForumPost, e.member, `{"title":"x"}`, e.postParams(e.chID, e.gone), "", "not found"},
		{"update post blank title", (*Handler).updateForumPost, e.member, `{"title":"  "}`, e.postParams(e.chID, e.msgID), "", ""},
		{"update post foreign tag", (*Handler).updateForumPost, e.member, foreignTagUpdate, e.postParams(e.chID, e.msgID), "", "tag does not belong"},
	}
}

func TestForumHandlersRejectBadRequests(t *testing.T) {
	f, h, mod, ch := forumCtx(t)
	tag, err := CreateForumTag(context.Background(), f.p, ch.ID, "known", "")
	if err != nil {
		t.Fatal(err)
	}
	post := forumMakePost(t, h, f.u, ch, "A post")
	env := forumBadEnv{
		member: f.u, mod: mod, chID: ch.ID.String(), textID: f.c.ID.String(),
		tagID: tag.ID.String(), msgID: post.ID.String(), gone: uuid.NewString(),
	}
	cases := append(forumBadTagCases(env), forumBadPostCases(env)...)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.fn(h, httptest.NewRecorder(), chatRequest(tc.user, tc.body, tc.params, tc.query))
			if err == nil {
				t.Fatal("expected an error")
			}
			if tc.want != "" && !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want containing %q", err, tc.want)
			}
		})
	}
}

func TestForumTagCapAndPostTagLimit(t *testing.T) {
	f, h, mod, ch := forumCtx(t)
	ids := make([]uuid.UUID, 0, 20)
	for i := 0; i < 20; i++ {
		tag, err := CreateForumTag(context.Background(), f.p, ch.ID, fmt.Sprintf("t%02d", i), "")
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, tag.ID)
	}
	cp := map[string]string{"channelID": ch.ID.String()}
	err := h.createForumTag(httptest.NewRecorder(), chatRequest(mod, `{"name":"overflow"}`, cp, ""))
	requireChatError(t, err, "at most 20 tags")

	body, _ := json.Marshal(map[string]any{"title": "Too tagged", "content": "c", "tag_ids": ids[:6]})
	err = h.createForumPost(httptest.NewRecorder(), chatRequest(f.u, string(body), cp, ""))
	requireChatError(t, err, "at most 5 tags")

	post := forumMakePost(t, h, f.u, ch, "Five tags", ids[:5]...)
	if len(post.Tags) != 5 {
		t.Fatalf("post carries %d tags, want 5", len(post.Tags))
	}
	pp := map[string]string{"channelID": ch.ID.String(), "messageID": post.ID.String()}
	body, _ = json.Marshal(map[string]any{"tag_ids": ids[:6]})
	err = h.updateForumPost(httptest.NewRecorder(), chatRequest(f.u, string(body), pp, ""))
	requireChatError(t, err, "at most 5 tags")
}

func TestForumTagsAreChannelScoped(t *testing.T) {
	f, h, mod, ch := forumCtx(t)
	other, err := CreateChannel(context.Background(), f.p, nil, "second-forum", ChannelTypeForum, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := CreateForumTag(context.Background(), f.p, other.ID, "foreign", "")
	if err != nil {
		t.Fatal(err)
	}
	own, err := CreateForumTag(context.Background(), f.p, ch.ID, "own", "")
	if err != nil {
		t.Fatal(err)
	}
	cp := map[string]string{"channelID": ch.ID.String()}
	body := fmt.Sprintf(`{"title":"t","content":"c","tag_ids":["%s"]}`, foreign.ID)
	requireChatError(t, h.createForumPost(httptest.NewRecorder(), chatRequest(f.u, body, cp, "")), "tag does not belong")

	tp := map[string]string{"channelID": ch.ID.String(), "tagID": foreign.ID.String()}
	requireChatError(t, h.updateForumTag(httptest.NewRecorder(), chatRequest(mod, `{"name":"x"}`, tp, "")), "not found")
	requireChatError(t, h.deleteForumTag(httptest.NewRecorder(), chatRequest(mod, "", tp, "")), "not found")

	rec := httptest.NewRecorder()
	if err := h.listForumTags(rec, chatRequest(f.u, "", cp, "")); err != nil {
		t.Fatal(err)
	}
	tags := forumDecode[[]ForumTag](t, rec)
	if len(tags) != 1 || tags[0].ID != own.ID {
		t.Fatalf("listing leaked tags from another forum: %+v", tags)
	}
}

func TestForumTagPartialUpdates(t *testing.T) {
	f, _, _, ch := forumCtx(t)
	ctx := context.Background()
	tag, err := CreateForumTag(ctx, f.p, ch.ID, "orig", "A")
	if err != nil {
		t.Fatal(err)
	}
	newName, newEmoji, empty := "renamed", "B", ""
	got, _, err := UpdateForumTag(ctx, f.p, ch.ID, tag.ID, TagPatch{Name: &newName})
	if err != nil || got.Name != "renamed" || got.Emoji != "A" {
		t.Fatalf("name-only update = %+v, %v", got, err)
	}
	got, _, err = UpdateForumTag(ctx, f.p, ch.ID, tag.ID, TagPatch{Emoji: &newEmoji})
	if err != nil || got.Name != "renamed" || got.Emoji != "B" {
		t.Fatalf("emoji-only update = %+v, %v", got, err)
	}
	got, _, err = UpdateForumTag(ctx, f.p, ch.ID, tag.ID, TagPatch{Emoji: &empty})
	if err != nil || got.Emoji != "" {
		t.Fatalf("clearing emoji = %+v, %v", got, err)
	}
}

func TestForumDeletingTagKeepsPostsAndClearsFilter(t *testing.T) {
	f, h, mod, ch := forumCtx(t)
	tag, err := CreateForumTag(context.Background(), f.p, ch.ID, "doomed", "")
	if err != nil {
		t.Fatal(err)
	}
	tagged := forumMakePost(t, h, f.u, ch, "Tagged", tag.ID)
	plain := forumMakePost(t, h, f.u, ch, "Plain")
	requireIDs(t, "before delete", forumListIDs(t, h, f.u, ch, "tag_id="+tag.ID.String()), tagged)

	tp := map[string]string{"channelID": ch.ID.String(), "tagID": tag.ID.String()}
	rec := httptest.NewRecorder()
	if err := h.deleteForumTag(rec, chatRequest(mod, "", tp, "")); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if got := forumListIDs(t, h, f.u, ch, "tag_id="+tag.ID.String()); len(got) != 0 {
		t.Fatalf("filter by deleted tag returned %d posts", len(got))
	}
	requireIDs(t, "all posts survive", forumListIDs(t, h, f.u, ch, "sort=newest"), plain, tagged)
	msg, err := GetMessage(context.Background(), f.p, tagged.ID)
	if err != nil || len(msg.Tags) != 0 {
		t.Fatalf("post still carries tags: %+v, %v", msg, err)
	}
}

func TestForumPostListingSortAndPaging(t *testing.T) {
	f, h, _, ch := forumCtx(t)
	a := forumMakePost(t, h, f.u, ch, "A")
	b := forumMakePost(t, h, f.u, ch, "B")
	c := forumMakePost(t, h, f.u, ch, "C")
	if err := forumReply(t, h, f.u, ch, a.ID); err != nil {
		t.Fatal(err)
	}

	requireIDs(t, "default is latest activity", forumListIDs(t, h, f.u, ch, ""), a, c, b)
	requireIDs(t, "activity", forumListIDs(t, h, f.u, ch, "sort=activity"), a, c, b)
	requireIDs(t, "newest", forumListIDs(t, h, f.u, ch, "sort=newest"), c, b, a)
	requireIDs(t, "creation", forumListIDs(t, h, f.u, ch, "sort=creation"), c, b, a)
	requireIDs(t, "newest before c", forumListIDs(t, h, f.u, ch, "sort=newest&before="+c.ID.String()), b, a)
	requireIDs(t, "activity before a", forumListIDs(t, h, f.u, ch, "before="+a.ID.String()), c, b)
	requireIDs(t, "limit", forumListIDs(t, h, f.u, ch, "sort=newest&limit=2"), c, b)
	if got := forumListIDs(t, h, f.u, ch, "sort=newest&limit=500"); len(got) != 3 {
		t.Fatalf("oversized limit returned %d posts", len(got))
	}
}

func TestForumListingOmitsRepliesAndStoreClampsLimit(t *testing.T) {
	f, h, _, ch := forumCtx(t)
	a := forumMakePost(t, h, f.u, ch, "Only root")
	if err := forumReply(t, h, f.u, ch, a.ID); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"newest", "activity"} {
		for _, limit := range []int{0, 1000} {
			posts, err := ListForumPosts(context.Background(), f.p, ch.ID, mode, nil, HistoryQuery{Limit: limit})
			if err != nil || len(posts) != 1 || posts[0].ID != a.ID {
				t.Fatalf("%s/%d: got %d posts, err %v", mode, limit, len(posts), err)
			}
		}
	}
}

func TestForumPostUpdateRules(t *testing.T) {
	f, h, mod, ch := forumCtx(t)
	other := forumNewUser(t, f, "forum_other")
	tag, err := CreateForumTag(context.Background(), f.p, ch.ID, "t1", "")
	if err != nil {
		t.Fatal(err)
	}
	post := forumMakePost(t, h, f.u, ch, "Mine")
	pp := map[string]string{"channelID": ch.ID.String(), "messageID": post.ID.String()}
	update := func(u *auth.User, body string) (*httptest.ResponseRecorder, error) {
		rec := httptest.NewRecorder()
		return rec, h.updateForumPost(rec, chatRequest(u, body, pp, ""))
	}

	tagBody := fmt.Sprintf(`{"tag_ids":["%s"]}`, tag.ID)
	rec, err := update(f.u, tagBody)
	if err != nil || len(forumDecode[Message](t, rec).Tags) != 1 {
		t.Fatalf("author tagging failed: %v", err)
	}
	if _, err := update(other, tagBody); err == nil {
		t.Fatal("non-author must not retag a post")
	}
	if _, err := update(f.u, `{"is_locked":true}`); err == nil {
		t.Fatal("author without Manage messages must not lock own post")
	}
	rec, err = update(f.u, `{"tag_ids":[]}`)
	if err != nil || len(forumDecode[Message](t, rec).Tags) != 0 {
		t.Fatalf("clearing tags failed: %v", err)
	}
	if _, err := update(mod, `{"is_locked":true}`); err != nil {
		t.Fatalf("moderator lock: %v", err)
	}
	if err := forumReply(t, h, other, ch, post.ID); err == nil {
		t.Fatal("reply to locked post must fail")
	}
	if _, err := update(mod, `{"is_locked":false}`); err != nil {
		t.Fatalf("moderator unlock: %v", err)
	}
	if err := forumReply(t, h, other, ch, post.ID); err != nil {
		t.Fatalf("reply after unlock: %v", err)
	}
	if _, err = update(f.u, `{}`); err == nil || !strings.Contains(err.Error(), "no fields to update") {
		t.Fatalf("empty update error = %v; want no fields to update", err)
	}
}

func TestForumPostUpdateRejectsRepliesAndOtherChannels(t *testing.T) {
	f, h, _, ch := forumCtx(t)
	other, err := CreateChannel(context.Background(), f.p, nil, "elsewhere", ChannelTypeForum, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	post := forumMakePost(t, h, f.u, ch, "Root")
	if err := forumReply(t, h, f.u, ch, post.ID); err != nil {
		t.Fatal(err)
	}
	replyID := forumReplyID(t, f, post.ID)
	cases := map[string]map[string]string{
		"reply":         {"channelID": ch.ID.String(), "messageID": replyID.String()},
		"other channel": {"channelID": other.ID.String(), "messageID": post.ID.String()},
	}
	for name, params := range cases {
		t.Run(name, func(t *testing.T) {
			err := h.updateForumPost(httptest.NewRecorder(), chatRequest(f.u, `{"title":"x"}`, params, ""))
			requireChatError(t, err, "not found")
		})
	}
}

func TestForumPostUpdateWithoutPermissionService(t *testing.T) {
	f, _, mod, ch := forumCtx(t)
	h := &Handler{DB: f.p, Events: &events.Recorder{}}
	other := forumNewUser(t, f, "forum_plain")
	post, err := CreateForumPost(context.Background(), f.p, nil, ch.ID, f.u.ID, false, "Legacy", "content", nil)
	if err != nil {
		t.Fatal(err)
	}
	pp := map[string]string{"channelID": ch.ID.String(), "messageID": post.String()}
	if err := h.updateForumPost(httptest.NewRecorder(), chatRequest(other, `{"title":"x"}`, pp, "")); err == nil {
		t.Fatal("plain member must not edit someone else's post")
	}
	if err := h.updateForumPost(httptest.NewRecorder(), chatRequest(mod, `{"is_locked":true}`, pp, "")); err != nil {
		t.Fatalf("admin role should manage posts: %v", err)
	}
}

func TestForumRepliesAndRootRulesInForums(t *testing.T) {
	f, h, _, ch := forumCtx(t)
	cp := map[string]string{"channelID": ch.ID.String()}
	post := forumMakePost(t, h, f.u, ch, "Thread")

	if err := forumReply(t, h, f.u, ch, post.ID); err != nil {
		t.Fatalf("reply to post: %v", err)
	}
	replyID := forumReplyID(t, f, post.ID)
	if err := forumReply(t, h, f.u, ch, replyID); err == nil {
		t.Fatal("replying to a reply must fail")
	}
	body := `{"title":"Sneaky","content":"root via messages"}`
	if err := h.createMessage(httptest.NewRecorder(), chatRequest(f.u, body, cp, "")); err == nil {
		t.Fatal("root message through the plain endpoint must fail in a forum")
	}
	tp := map[string]string{"channelID": f.c.ID.String()}
	if err := h.createForumPost(httptest.NewRecorder(), chatRequest(f.u, body, tp, "")); err == nil {
		t.Fatal("forum post endpoint must refuse text channels")
	}
}

func TestForumValidationHelpers(t *testing.T) {
	if _, err := CleanTagName(strings.Repeat("é", 21)); err == nil {
		t.Fatal("21-rune tag name accepted")
	}
	if got, err := CleanTagName("  ok  "); err != nil || got != "ok" {
		t.Fatalf("CleanTagName = %q, %v", got, err)
	}
	if got, err := CleanTagEmoji(""); err != nil || got != "" {
		t.Fatalf("empty emoji = %q, %v", got, err)
	}
	if _, err := CleanTagEmoji(strings.Repeat("e", 33)); err == nil {
		t.Fatal("33-byte emoji accepted")
	}
	if _, err := CleanPostTitle(strings.Repeat("t", 101)); err == nil {
		t.Fatal("101-char title accepted")
	}
	if _, err := CleanPostTitle(" \t "); err == nil {
		t.Fatal("blank title accepted")
	}
	if got, err := CleanPostTitle(strings.Repeat("\u00fc", 100)); err != nil || got == "" {
		t.Fatalf("100-rune title rejected: %v", err)
	}
}

func TestForumStoreErrorsOnCanceledContext(t *testing.T) {
	f, _, _, ch := forumCtx(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	name := "x"
	calls := map[string]func() error{
		"list tags":     func() error { _, err := ListForumTags(ctx, f.p, ch.ID); return err },
		"validate tags": func() error { return ValidatePostTags(ctx, f.p, ch.ID, []uuid.UUID{uuid.New()}) },
		"delete tag":    func() error { _, err := DeleteForumTag(ctx, f.p, ch.ID, uuid.New()); return err },
		"update tag": func() error {
			_, _, err := UpdateForumTag(ctx, f.p, ch.ID, uuid.New(), TagPatch{Name: &name})
			return err
		},
		"create tag": func() error { _, err := CreateForumTag(ctx, f.p, ch.ID, "x", ""); return err },
		"list posts": func() error {
			_, err := ListForumPosts(ctx, f.p, ch.ID, "newest", nil, HistoryQuery{})
			return err
		},
		"update post": func() error {
			_, err := UpdateForumPost(ctx, f.p, ch.ID, uuid.New(), PostActor{ID: f.u.ID, CanManage: true}, PostPatch{})
			return err
		},
	}
	for label, call := range calls {
		if err := call(); err == nil {
			t.Fatalf("%s ignored the canceled context", label)
		}
	}
}
