//go:build integration

package chat

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat/channels"
	"github.com/msch128/mnema-talk/internal/chat/core"
)

func TestChannelEditsAreAllOrNothing(t *testing.T) {
	f := newChatFixture(t)
	ctx := context.Background()
	if _, err := channels.CreateChannelWithStage(ctx, f.p, nil, "text-stage", core.ChannelTypeText, "", 0, true); err == nil {
		t.Fatal("a text channel was created as a Stage")
	}
	var n int
	if err := f.p.QueryRow(ctx, `SELECT COUNT(*) FROM channels WHERE name = 'text-stage'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("channels named text-stage = %d, %v; want none", n, err)
	}

	voice, err := channels.CreateChannelWithStage(ctx, f.p, nil, "talk", core.ChannelTypeVoice, "", 0, false)
	if err != nil {
		t.Fatal(err)
	}
	stage, bad := true, -1
	if _, err := channels.EditChannel(ctx, f.p, voice.ID, nil, nil, &bad, nil, nil, &stage); err == nil {
		t.Fatal("invalid limit accepted")
	}
	if _, err := channels.EditChannel(ctx, f.p, f.c.ID, nil, nil, nil, nil, nil, &stage); err == nil {
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
	got, err := channels.RenameCategory(ctx, f.p, id, "  Games  ")
	if err != nil || got != "Games" {
		t.Fatalf("RenameCategory = %q, %v; want Games", got, err)
	}
}
