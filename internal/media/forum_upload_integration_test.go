//go:build integration

package media

import (
	"context"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat"
)

// A file uploaded with a title and tag ids starts a forum post, by the rules of
// a post made from text.
func TestUploadStartsAForumPost(t *testing.T) {
	f := mediaDB(t)
	f.h.MaxUploadBytes = 1 << 20
	ctx := context.Background()
	forum := uuid.New()
	execMedia(t, f.p, `INSERT INTO channels(id,name,type) VALUES($1,'help','forum')`, forum)
	tag, err := chat.CreateForumTag(ctx, f.p, forum, "bug", "")
	if err != nil {
		t.Fatal(err)
	}

	upload := func(channel uuid.UUID, parts ...formPart) error {
		all := append([]formPart{{name: "file", filename: "a.txt", mime: "text/plain", value: "plain text"}}, parts...)
		return f.h.upload(httptest.NewRecorder(), f.req(uploadRequest(t, all...), "channelID", channel.String()))
	}

	if err := upload(forum, formPart{name: "content", value: "see attached"}); err == nil {
		t.Fatal("a forum root without a title must be refused")
	}
	if err := upload(forum, formPart{name: "title", value: "Crash on start"}, formPart{name: "tag_ids", value: "nope"}); err == nil {
		t.Fatal("a malformed tag list must be refused")
	}
	if err := upload(forum, formPart{name: "title", value: "Crash on start"}, formPart{name: "tag_ids", value: uuid.NewString()}); err == nil {
		t.Fatal("a tag of no forum must be refused")
	}
	if err := upload(f.channel, formPart{name: "title", value: "Not a forum"}); err == nil {
		t.Fatal("a title outside a forum must be refused")
	}

	if err := upload(forum, formPart{name: "title", value: "Crash on start"}, formPart{name: "content", value: "see attached"}, formPart{name: "tag_ids", value: tag.ID.String()}); err != nil {
		t.Fatalf("post with a file: %v", err)
	}
	var title string
	var tags, files int
	err = f.p.QueryRow(ctx, `
		SELECT m.title, (SELECT COUNT(*) FROM forum_post_tags WHERE message_id = m.id), (SELECT COUNT(*) FROM media WHERE message_id = m.id)
		FROM messages m WHERE m.channel_id = $1 AND m.parent_id IS NULL`, forum).Scan(&title, &tags, &files)
	if err != nil || title != "Crash on start" || tags != 1 || files != 1 {
		t.Fatalf("stored post: %q tags=%d files=%d err=%v", title, tags, files, err)
	}

	// The follow that a post made from text gives its author comes with it.
	var follows int
	if err := f.p.QueryRow(ctx, `SELECT COUNT(*) FROM thread_follows tf JOIN messages m ON m.id = tf.thread_id WHERE m.channel_id = $1 AND tf.user_id = $2`, forum, f.user.ID).Scan(&follows); err != nil || follows != 1 {
		t.Fatal(fmt.Sprintf("author follows: %d %v", follows, err))
	}

	// A required tag holds for uploads too.
	if _, _, err := chat.UpdateForumSettings(ctx, f.p, forum, chat.ForumSettingsPatch{RequireTag: ptr(true)}); err != nil {
		t.Fatal(err)
	}
	if err := upload(forum, formPart{name: "title", value: "No tag"}); err == nil {
		t.Fatal("a forum that requires a tag refuses a post without one")
	}
}

func ptr[T any](v T) *T { return &v }
