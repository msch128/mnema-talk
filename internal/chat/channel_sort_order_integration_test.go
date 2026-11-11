//go:build integration

package chat

import (
	"context"
	"net/http"
	"testing"

	"github.com/msch128/mnema-talk/internal/chat/channels"
	"github.com/msch128/mnema-talk/internal/chat/core"
)

func TestSortOrderBeyondTheColumnIsRefused(t *testing.T) {
	f := newChatFixture(t)
	ctx := context.Background()
	if _, err := channels.CreateCategory(ctx, f.p, "big", 3_000_000_000); apiStatus(err) != http.StatusBadRequest {
		t.Fatalf("category: %v", err)
	}
	if _, err := channels.CreateChannelWithStage(ctx, f.p, nil, "big", core.ChannelTypeText, "", -3_000_000_000, false); apiStatus(err) != http.StatusBadRequest {
		t.Fatalf("channel: %v", err)
	}
	cat, err := channels.CreateCategory(ctx, f.p, "ok", channels.MaxSortOrder)
	if err != nil {
		t.Fatal(err)
	}
	err = channels.ApplyLayout(ctx, f.p, []channels.CategoryOrder{{ID: cat.ID, SortOrder: 3_000_000_000}}, nil)
	if apiStatus(err) != http.StatusBadRequest {
		t.Fatalf("category order: %v", err)
	}
	err = channels.ApplyLayout(ctx, f.p, nil, []channels.ChannelPlacement{{ID: f.c.ID, SortOrder: 3_000_000_000}})
	if apiStatus(err) != http.StatusBadRequest {
		t.Fatalf("channel placement: %v", err)
	}
}
