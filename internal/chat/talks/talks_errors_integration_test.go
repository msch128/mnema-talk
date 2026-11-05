//go:build integration

package talks

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat/channels"
	"github.com/msch128/mnema-talk/internal/chat/chattest"
	"github.com/msch128/mnema-talk/internal/chat/core"
)

func canceledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func TestATransientLookupErrorDoesNotCountAsAnAlreadyRemovedTalk(t *testing.T) {
	w := newTalksWorld(t)
	talk, err := w.mgr.Open(context.Background(), w.alice.ID, w.lobby.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.mgr.remove(canceledContext(), talk.ID, "empty"); err == nil {
		t.Fatal("remove reported success although the lookup failed")
	}
	if !w.exists(t, talk.ID) {
		t.Fatal("the Talk vanished")
	}
	if err := w.mgr.remove(context.Background(), uuid.New(), "empty"); err != nil {
		t.Fatalf("a Talk that is gone is no error: %v", err)
	}
}

func TestFillTalkFieldsReportsFailures(t *testing.T) {
	w := newTalksWorld(t)
	ch := &core.Channel{ID: w.lobby.ID}
	if err := channels.FillTalkFields(canceledContext(), w.p, ch); err == nil {
		t.Fatal("a failed query was swallowed")
	}
	if err := channels.FillTalkFields(context.Background(), w.p, ch); err != nil || !ch.IsLobby {
		t.Fatalf("err = %v, lobby = %v", err, ch.IsLobby)
	}
	if err := channels.FillTalkFields(context.Background(), w.p, &core.Channel{ID: uuid.New()}); !errors.Is(err, core.ErrChannelNotFound) {
		t.Fatalf("err = %v; want not found", err)
	}
}

func TestPostTalkInviteOnlyMapsAMissingTalkToNotFound(t *testing.T) {
	w := newTalksWorld(t)
	h := &API{DB: w.p}
	_, err := h.postTalkInvite(canceledContext(), uuid.New(), w.alice.ID, w.lobby.ID)
	if err == nil || errors.Is(err, core.ErrChannelNotFound) {
		t.Fatalf("err = %v; a database failure must not read as not found", err)
	}
	if _, err := h.postTalkInvite(context.Background(), uuid.New(), w.alice.ID, uuid.New()); !errors.Is(err, core.ErrChannelNotFound) {
		t.Fatalf("err = %v; want not found", err)
	}
}

func TestDMSendContextOnlyMapsAMissingConversationToNotFound(t *testing.T) {
	w := newTalksWorld(t)
	h := &API{DB: w.p}
	r := chattest.Request(w.alice, "", nil, "").WithContext(canceledContext())
	if _, err := h.dmSendContext(r, w.lobby.ID); err == nil || errors.Is(err, core.ErrChannelNotFound) {
		t.Fatalf("err = %v; a database failure must not read as not found", err)
	}
	r = chattest.Request(w.alice, "", nil, "")
	if _, err := h.dmSendContext(r, uuid.New()); !errors.Is(err, core.ErrChannelNotFound) {
		t.Fatalf("unknown conversation: err = %v; want not found", err)
	}
}
