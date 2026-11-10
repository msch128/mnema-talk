//go:build integration

package messages

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat/chattest"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/perm"
)

func TestFollowingAgainMovesAThreadToTheFront(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	second, err := CreateMessage(ctx, f.p, NewMessage{ChannelID: f.ch, UserID: f.u.ID, Content: "second"})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []uuid.UUID{f.msg, second} {
		if err := SetThreadFollow(ctx, f.p, f.u.ID, id, true); err != nil {
			t.Fatal(err)
		}
	}
	if err := SetThreadFollow(ctx, f.p, f.u.ID, f.msg, false); err != nil {
		t.Fatal(err)
	}
	if err := SetThreadFollow(ctx, f.p, f.u.ID, f.msg, true); err != nil {
		t.Fatal(err)
	}
	// Following twice keeps the position.
	if err := SetThreadFollow(ctx, f.p, f.u.ID, f.msg, true); err != nil {
		t.Fatal(err)
	}
	h := &API{DB: f.p, Guard: core.NewGuard(perm.NewService(f.p))}
	rec := httptest.NewRecorder()
	if err := h.ListFollowedThreads(rec, chattest.Request(f.u, "", nil, "")); err != nil {
		t.Fatal(err)
	}
	body := rec.Body.String()
	if strings.Index(body, f.msg.String()) > strings.Index(body, second.String()) || !strings.Contains(body, second.String()) {
		t.Fatalf("a re-followed thread is not first: %s", body)
	}
}
