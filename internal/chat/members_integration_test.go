//go:build integration

package chat

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat/core"
)

func TestGetMemberReadsOneVisibleMember(t *testing.T) {
	f := newChatFixture(t)
	ctx := context.Background()
	m, err := core.GetMember(ctx, f.p, f.u.ID)
	if err != nil || m.ID != f.u.ID || m.Username != f.u.Username {
		t.Fatalf("member: %+v %v", m, err)
	}
	if _, err := core.GetMember(ctx, f.p, uuid.New()); !errors.Is(err, core.ErrUnknownUser) {
		t.Fatalf("unknown member: %v", err)
	}
}
