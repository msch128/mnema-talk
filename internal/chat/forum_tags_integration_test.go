//go:build integration

package chat

import (
	"context"
	"net/http"
	"testing"

	"github.com/msch128/mnema-talk/internal/chat/forum"
)

func TestForumTagNameConflictIsReported(t *testing.T) {
	f, _, _ := setupForumEnv(t)
	ch := createTestForum(t, f)
	ctx := context.Background()
	if _, err := forum.CreateForumTagSpec(ctx, f.p, ch.ID, forum.TagSpec{Name: "Bug"}); err != nil {
		t.Fatal(err)
	}
	_, err := forum.CreateForumTagSpec(ctx, f.p, ch.ID, forum.TagSpec{Name: "Bug"})
	requireAPIStatus(t, err, http.StatusConflict)
	other, err := forum.CreateForumTagSpec(ctx, f.p, ch.ID, forum.TagSpec{Name: "Question"})
	if err != nil {
		t.Fatal(err)
	}
	name := "Bug"
	_, _, err = forum.UpdateForumTag(ctx, f.p, ch.ID, other.ID, forum.TagPatch{Name: &name})
	requireAPIStatus(t, err, http.StatusConflict)
}

func TestForumTagUpdatesDoNotOverwriteEachOther(t *testing.T) {
	f, _, _ := setupForumEnv(t)
	ch := createTestForum(t, f)
	ctx := context.Background()
	tag, err := forum.CreateForumTagSpec(ctx, f.p, ch.ID, forum.TagSpec{Name: "old", Emoji: "A"})
	if err != nil {
		t.Fatal(err)
	}
	tx, err := f.p.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	name, emoji := "new", "B"
	if _, _, err := forum.UpdateForumTag(ctx, tx, ch.ID, tag.ID, forum.TagPatch{Name: &name}); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, _, err := forum.UpdateForumTag(ctx, f.p, ch.ID, tag.ID, forum.TagPatch{Emoji: &emoji})
		done <- err
	}()
	waitForBlockedStatement(t, f.p) // the second patch has read the tag and waits for the row
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	tags, err := forum.ListForumTags(ctx, f.p, ch.ID)
	if err != nil || len(tags) != 1 || tags[0].Name != "new" || tags[0].Emoji != "B" {
		t.Fatalf("tags = %+v, %v; want name new and emoji B", tags, err)
	}
}
