//go:build integration

package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/perm"
)

func setupForumEnv(t *testing.T) (chatFixture, *Handler, *auth.User) {
	t.Helper()
	f := newChatFixture(t)
	perms := perm.NewService(f.p)
	h := &Handler{DB: f.p, Events: &events.Recorder{}, Perms: perms}

	mod := &auth.User{Username: "forum_mod", DisplayName: "Forum Mod", Role: auth.RoleAdmin}
	if err := f.p.QueryRow(context.Background(), `INSERT INTO users (username, display_name, password_hash, role) VALUES ($1,$2,'x','admin') RETURNING id, created_at`, mod.Username, mod.DisplayName).Scan(&mod.ID, &mod.CreatedAt); err != nil {
		t.Fatal(err)
	}
	return f, h, mod
}

func createTestForum(t *testing.T, f chatFixture) *Channel {
	t.Helper()
	ch, err := CreateChannel(context.Background(), f.p, nil, "general-forum", ChannelTypeForum, "Forum topic", 0)
	if err != nil {
		t.Fatalf("create forum channel: %v", err)
	}
	return ch
}

func TestForumTagsCRUD(t *testing.T) {
	f, h, mod := setupForumEnv(t)
	ch := createTestForum(t, f)
	chParam := map[string]string{"channelID": ch.ID.String()}

	// Non-admin cannot create tags (ManageChannels required)
	req := chatRequest(f.u, `{"name":"bugs","emoji":"🐛"}`, chParam, "")
	if err := h.createForumTag(httptest.NewRecorder(), req); err == nil {
		t.Fatal("expected forbidden for normal user creating tag")
	}

	// Admin creates tag
	rec := httptest.NewRecorder()
	req = chatRequest(mod, `{"name":"bugs","emoji":"🐛"}`, chParam, "")
	if err := h.createForumTag(rec, req); err != nil {
		t.Fatalf("admin create tag: %v", err)
	}
	var tag ForumTag
	if err := json.Unmarshal(rec.Body.Bytes(), &tag); err != nil {
		t.Fatal(err)
	}
	if tag.Name != "bugs" || tag.Emoji != "🐛" {
		t.Fatalf("unexpected tag: %+v", tag)
	}

	// Update tag
	tagParam := map[string]string{"channelID": ch.ID.String(), "tagID": tag.ID.String()}
	rec = httptest.NewRecorder()
	req = chatRequest(mod, `{"name":"critical-bugs","emoji":"🔥"}`, tagParam, "")
	if err := h.updateForumTag(rec, req); err != nil {
		t.Fatalf("admin update tag: %v", err)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &tag); err != nil {
		t.Fatal(err)
	}
	if tag.Name != "critical-bugs" || tag.Emoji != "🔥" {
		t.Fatalf("expected updated tag, got: %+v", tag)
	}

	// Delete tag
	req = chatRequest(mod, "", tagParam, "")
	if err := h.deleteForumTag(httptest.NewRecorder(), req); err != nil {
		t.Fatalf("admin delete tag: %v", err)
	}
}

func TestForumTagsLimit(t *testing.T) {
	f, _, _ := setupForumEnv(t)
	ch := createTestForum(t, f)

	// Create 20 tags
	for i := 0; i < 20; i++ {
		name := fmt.Sprintf("tag-%02d", i)
		if _, err := CreateForumTag(context.Background(), f.p, ch.ID, name, "🏷️"); err != nil {
			t.Fatalf("failed to create tag %d: %v", i, err)
		}
	}

	// 21st tag must fail
	if _, err := CreateForumTag(context.Background(), f.p, ch.ID, "tag-overflow", "🏷️"); err == nil {
		t.Fatal("expected error on 21st tag")
	}
}

func TestForumPostCreationAndValidation(t *testing.T) {
	f, h, _ := setupForumEnv(t)
	ch := createTestForum(t, f)
	chParam := map[string]string{"channelID": ch.ID.String()}

	// Empty title fails
	req := chatRequest(f.u, `{"title":"","content":"First post content"}`, chParam, "")
	if err := h.createForumPost(httptest.NewRecorder(), req); err == nil {
		t.Fatal("expected error with empty title")
	}

	// Empty content fails
	req = chatRequest(f.u, `{"title":"Hello Forum","content":"   "}`, chParam, "")
	if err := h.createForumPost(httptest.NewRecorder(), req); err == nil {
		t.Fatal("expected error with blank content")
	}

	// Valid post succeeds
	rec := httptest.NewRecorder()
	req = chatRequest(f.u, `{"title":"Getting Started","content":"Welcome to the new forum!"}`, chParam, "")
	if err := h.createForumPost(rec, req); err != nil {
		t.Fatalf("create forum post: %v", err)
	}
	var post Message
	if err := json.Unmarshal(rec.Body.Bytes(), &post); err != nil {
		t.Fatal(err)
	}
	if post.Title == nil || *post.Title != "Getting Started" {
		t.Fatalf("unexpected post title: %v", post.Title)
	}

	// Creator should automatically follow the thread
	following, err := IsFollowingThread(context.Background(), f.p, f.u.ID, post.ID)
	if err != nil || !following {
		t.Fatalf("expected creator to follow post thread: %v", err)
	}

	// Ordinary message creation without title in forum channel must fail
	reqMsg := chatRequest(f.u, `{"content":"Root message without title"}`, chParam, "")
	if err := h.createMessage(httptest.NewRecorder(), reqMsg); err == nil {
		t.Fatal("expected root message without title in forum to fail")
	}
}

func TestForumPostTagsAndFiltering(t *testing.T) {
	f, h, _ := setupForumEnv(t)
	ch := createTestForum(t, f)
	chParam := map[string]string{"channelID": ch.ID.String()}

	tag1, err := CreateForumTag(context.Background(), f.p, ch.ID, "discussion", "💬")
	if err != nil {
		t.Fatal(err)
	}
	tag2, err := CreateForumTag(context.Background(), f.p, ch.ID, "feedback", "💡")
	if err != nil {
		t.Fatal(err)
	}

	// Create post with tag1
	rec := httptest.NewRecorder()
	body1 := fmt.Sprintf(`{"title":"Discussion Post","content":"Content 1","tag_ids":["%s"]}`, tag1.ID)
	if err := h.createForumPost(rec, chatRequest(f.u, body1, chParam, "")); err != nil {
		t.Fatal(err)
	}
	var p1 Message
	if err := json.Unmarshal(rec.Body.Bytes(), &p1); err != nil {
		t.Fatal(err)
	}
	if len(p1.Tags) != 1 || p1.Tags[0].ID != tag1.ID {
		t.Fatalf("expected tag1, got %+v", p1.Tags)
	}

	// Create post with tag2
	body2 := fmt.Sprintf(`{"title":"Feedback Post","content":"Content 2","tag_ids":["%s"]}`, tag2.ID)
	if err := h.createForumPost(httptest.NewRecorder(), chatRequest(f.u, body2, chParam, "")); err != nil {
		t.Fatal(err)
	}

	// Filter by tag1
	rec = httptest.NewRecorder()
	if err := h.listForumPosts(rec, chatRequest(f.u, "", chParam, "tag_id="+tag1.ID.String())); err != nil {
		t.Fatal(err)
	}
	var filtered []Message
	if err := json.Unmarshal(rec.Body.Bytes(), &filtered); err != nil {
		t.Fatal(err)
	}
	if len(filtered) != 1 || filtered[0].ID != p1.ID {
		t.Fatalf("expected 1 post matching tag1, got %d", len(filtered))
	}
}

func TestForumPostLockingAndReplies(t *testing.T) {
	f, h, mod := setupForumEnv(t)
	ch := createTestForum(t, f)
	chParam := map[string]string{"channelID": ch.ID.String()}

	// Create post
	rec := httptest.NewRecorder()
	if err := h.createForumPost(rec, chatRequest(f.u, `{"title":"Lock Test","content":"Topic"}`, chParam, "")); err != nil {
		t.Fatal(err)
	}
	var post Message
	if err := json.Unmarshal(rec.Body.Bytes(), &post); err != nil {
		t.Fatal(err)
	}
	postParam := map[string]string{"channelID": ch.ID.String(), "messageID": post.ID.String()}

	// Normal user cannot lock post
	req := chatRequest(f.u, `{"is_locked":true}`, postParam, "")
	if err := h.updateForumPost(httptest.NewRecorder(), req); err == nil {
		t.Fatal("expected non-moderator lock to fail")
	}

	// Moderator locks post
	rec = httptest.NewRecorder()
	req = chatRequest(mod, `{"is_locked":true}`, postParam, "")
	if err := h.updateForumPost(rec, req); err != nil {
		t.Fatalf("moderator lock post: %v", err)
	}

	// Normal user cannot reply to locked thread
	replyBody := fmt.Sprintf(`{"content":"I want to reply","parent_id":"%s"}`, post.ID)
	if err := h.createMessage(httptest.NewRecorder(), chatRequest(f.u, replyBody, chParam, "")); err == nil {
		t.Fatal("expected reply to locked post by normal user to fail")
	}

	// Moderator can reply to locked thread
	rec = httptest.NewRecorder()
	if err := h.createMessage(rec, chatRequest(mod, replyBody, chParam, "")); err != nil {
		t.Fatalf("expected moderator to reply to locked thread: %v", err)
	}

	// Activity sort: check that last_activity_at is updated
	rec = httptest.NewRecorder()
	if err := h.listForumPosts(rec, chatRequest(f.u, "", chParam, "sort=activity")); err != nil {
		t.Fatal(err)
	}
	var listed []Message
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed) == 0 || listed[0].ReplyCount != 1 {
		t.Fatalf("expected reply count 1, got %+v", listed)
	}
}

func TestForumPostEditTagsPermissions(t *testing.T) {
	f, h, mod := setupForumEnv(t)
	ch := createTestForum(t, f)
	chParam := map[string]string{"channelID": ch.ID.String()}

	// Another user
	other := &auth.User{Username: "other_user", DisplayName: "Other User", Role: auth.RoleUser}
	if err := f.p.QueryRow(context.Background(), `INSERT INTO users (username, display_name, password_hash) VALUES ($1,$2,'x') RETURNING id, created_at`, other.Username, other.DisplayName).Scan(&other.ID, &other.CreatedAt); err != nil {
		t.Fatal(err)
	}

	// f.u creates a post
	rec := httptest.NewRecorder()
	if err := h.createForumPost(rec, chatRequest(f.u, `{"title":"Owner Post","content":"Mine"}`, chParam, "")); err != nil {
		t.Fatal(err)
	}
	var post Message
	if err := json.Unmarshal(rec.Body.Bytes(), &post); err != nil {
		t.Fatal(err)
	}
	postParam := map[string]string{"channelID": ch.ID.String(), "messageID": post.ID.String()}

	// Author can edit post title
	rec = httptest.NewRecorder()
	if err := h.updateForumPost(rec, chatRequest(f.u, `{"title":"Updated Title"}`, postParam, "")); err != nil {
		t.Fatalf("author update title: %v", err)
	}

	// other_user cannot edit post title
	if err := h.updateForumPost(httptest.NewRecorder(), chatRequest(other, `{"title":"Hacked Title"}`, postParam, "")); err == nil {
		t.Fatal("expected other user editing post to fail")
	}

	// Moderator can edit post title
	rec = httptest.NewRecorder()
	if err := h.updateForumPost(rec, chatRequest(mod, `{"title":"Mod Edited Title"}`, postParam, "")); err != nil {
		t.Fatalf("moderator update title: %v", err)
	}
}

func TestForumChannelPermissions(t *testing.T) {
	f, h, _ := setupForumEnv(t)
	ch := createTestForum(t, f)
	chParam := map[string]string{"channelID": ch.ID.String()}

	var everyone uuid.UUID
	if err := f.p.QueryRow(context.Background(), `SELECT id FROM roles WHERE is_everyone`).Scan(&everyone); err != nil {
		t.Fatal(err)
	}

	// Deny ViewChannel
	if _, err := f.p.Exec(context.Background(), `INSERT INTO permission_overwrites (channel_id, role_id, deny) VALUES ($1, $2, $3)`, ch.ID, everyone, int64(perm.ViewChannel)); err != nil {
		t.Fatal(err)
	}
	h.Perms.Invalidate()

	if err := h.listForumTags(httptest.NewRecorder(), chatRequest(f.u, "", chParam, "")); err == nil {
		t.Fatal("expected list tags to fail without ViewChannel")
	}
	if err := h.listForumPosts(httptest.NewRecorder(), chatRequest(f.u, "", chParam, "")); err == nil {
		t.Fatal("expected list posts to fail without ViewChannel")
	}

	// Allow ViewChannel, Deny SendMessages
	if _, err := f.p.Exec(context.Background(), `DELETE FROM permission_overwrites WHERE channel_id = $1`, ch.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.p.Exec(context.Background(), `INSERT INTO permission_overwrites (channel_id, role_id, deny) VALUES ($1, $2, $3)`, ch.ID, everyone, int64(perm.SendMessages)); err != nil {
		t.Fatal(err)
	}
	h.Perms.Invalidate()

	if err := h.createForumPost(httptest.NewRecorder(), chatRequest(f.u, `{"title":"Blocked Post","content":"Can't post"}`, chParam, "")); err == nil {
		t.Fatal("expected create post to fail without SendMessages")
	}
}
