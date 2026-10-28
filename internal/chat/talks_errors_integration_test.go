//go:build integration

package chat

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
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
	ch := &Channel{ID: w.lobby.ID}
	if err := FillTalkFields(canceledContext(), w.p, ch); err == nil {
		t.Fatal("a failed query was swallowed")
	}
	if err := FillTalkFields(context.Background(), w.p, ch); err != nil || !ch.IsLobby {
		t.Fatalf("err = %v, lobby = %v", err, ch.IsLobby)
	}
	if err := FillTalkFields(context.Background(), w.p, &Channel{ID: uuid.New()}); !errors.Is(err, errChannelNotFound) {
		t.Fatalf("err = %v; want not found", err)
	}
}

func TestPostTalkInviteOnlyMapsAMissingTalkToNotFound(t *testing.T) {
	w := newTalksWorld(t)
	h := &Handler{DB: w.p}
	_, err := h.postTalkInvite(canceledContext(), uuid.New(), w.alice.ID, w.lobby.ID)
	if err == nil || errors.Is(err, errChannelNotFound) {
		t.Fatalf("err = %v; a database failure must not read as not found", err)
	}
	if _, err := h.postTalkInvite(context.Background(), uuid.New(), w.alice.ID, uuid.New()); !errors.Is(err, errChannelNotFound) {
		t.Fatalf("err = %v; want not found", err)
	}
}

func TestFollowingAgainMovesAThreadToTheFront(t *testing.T) {
	f := newChatFixture(t)
	ctx := context.Background()
	second, err := CreateMessage(ctx, f.p, NewMessage{ChannelID: f.c.ID, UserID: f.u.ID, Content: "second"})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []uuid.UUID{f.m, second} {
		if err := SetThreadFollow(ctx, f.p, f.u.ID, id, true); err != nil {
			t.Fatal(err)
		}
	}
	if err := SetThreadFollow(ctx, f.p, f.u.ID, f.m, false); err != nil {
		t.Fatal(err)
	}
	if err := SetThreadFollow(ctx, f.p, f.u.ID, f.m, true); err != nil {
		t.Fatal(err)
	}
	// Following twice keeps the position.
	if err := SetThreadFollow(ctx, f.p, f.u.ID, f.m, true); err != nil {
		t.Fatal(err)
	}
	h := &Handler{DB: f.p}
	rec := httptest.NewRecorder()
	if err := h.listFollowedThreads(rec, chatRequest(f.u, "", nil, "")); err != nil {
		t.Fatal(err)
	}
	body := rec.Body.String()
	if strings.Index(body, f.m.String()) > strings.Index(body, second.String()) || !strings.Contains(body, second.String()) {
		t.Fatalf("a re-followed thread is not first: %s", body)
	}
}
