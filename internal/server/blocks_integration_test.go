//go:build integration

package server

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/events"
)

type blockedList []struct {
	UserID   uuid.UUID `json:"user_id"`
	Username string    `json:"username"`
}

func (c *client) blocks(t *testing.T) blockedList {
	t.Helper()
	res := c.get("/api/me/blocks")
	wantStatus(t, "list blocks", res, http.StatusOK)
	var out blockedList
	res.decode(t, &out)
	return out
}

func TestBlockingIsPrivateIdempotentAndListedForTheBlockerOnly(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	alice, bob := a.register(admin, "alice"), a.register(admin, "bobby")
	target := "/api/me/blocks/" + bob.user.ID.String()

	wantStatus(t, "block", alice.put(target, nil), http.StatusNoContent)
	wantStatus(t, "block again", alice.put(target, nil), http.StatusNoContent)
	if got := alice.blocks(t); len(got) != 1 || got[0].UserID != bob.user.ID || got[0].Username != "bobby" {
		t.Fatalf("alice's blocks: %+v", got)
	}
	if got := bob.blocks(t); len(got) != 0 {
		t.Fatalf("the blocked member sees no trace: %+v", got)
	}

	// Only the blocker's own sessions hear about it, once.
	var told int
	for _, e := range a.events.Snapshot() {
		if e.Type != events.BlockUpdated {
			continue
		}
		told++
		if len(e.Recipients) != 1 || e.Recipients[0] != alice.user.ID {
			t.Errorf("block_updated went to %v", e.Recipients)
		}
	}
	if told != 1 {
		t.Errorf("block_updated sent %d times, want 1 (the repeat changed nothing)", told)
	}

	wantStatus(t, "unblock", alice.delete(target), http.StatusNoContent)
	wantStatus(t, "unblock again", alice.delete(target), http.StatusNoContent)
	if got := alice.blocks(t); len(got) != 0 {
		t.Fatalf("after unblock: %+v", got)
	}
}

func TestBlockingRefusesYourselfUnknownMembersAndBadIDs(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	alice := a.register(admin, "alice")

	wantStatus(t, "yourself", alice.put("/api/me/blocks/"+alice.user.ID.String(), nil), http.StatusBadRequest)
	wantStatus(t, "unknown", alice.put("/api/me/blocks/"+uuid.NewString(), nil), http.StatusNotFound)
	wantStatus(t, "bad id", alice.put("/api/me/blocks/nope", nil), http.StatusBadRequest)
	wantStatus(t, "signed out", a.anon().put("/api/me/blocks/"+admin.user.ID.String(), nil), http.StatusUnauthorized)
	// The Owner can be blocked like anybody else: it only mutes them for you.
	wantStatus(t, "owner", alice.put("/api/me/blocks/"+admin.user.ID.String(), nil), http.StatusNoContent)
}

func TestTheBlockListIsBounded(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	alice := a.register(admin, "alice")
	_, err := a.db.Exec(context.Background(), `
		WITH extra AS (
		    INSERT INTO users (username, display_name, password_hash)
		    SELECT 'filler' || n, 'Filler ' || n, 'x' FROM generate_series(1, $2::int) n RETURNING id)
		INSERT INTO user_blocks (blocker_id, blocked_id) SELECT $1, id FROM extra`, alice.user.ID, chat.MaxBlocksPerMember)
	if err != nil {
		t.Fatal(err)
	}
	wantStatus(t, "one too many", alice.put("/api/me/blocks/"+admin.user.ID.String(), nil), http.StatusConflict)
	// Blocking someone already blocked still works at the limit.
	var someone uuid.UUID
	if err := a.db.QueryRow(context.Background(), `SELECT blocked_id FROM user_blocks LIMIT 1`).Scan(&someone); err != nil {
		t.Fatal(err)
	}
	wantStatus(t, "repeat at the limit", alice.put("/api/me/blocks/"+someone.String(), nil), http.StatusNoContent)
}

func TestABlockedMembersMentionsAndRepliesLeaveNoTraceForTheBlocker(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	alice, bob, carol := a.register(admin, "alice"), a.register(admin, "bobby"), a.register(admin, "carol")
	ch := a.createChannel(admin, "general", "text")

	mine := a.send(alice, ch, "alice's own message")
	a.send(bob, ch, "hello @alice")
	wantStatus(t, "reply", bob.post("/api/channels/"+ch.String()+"/messages", map[string]any{"content": "a reply", "reply_to_id": mine}), http.StatusCreated)
	a.send(carol, ch, "hello @alice, from someone not blocked")

	inbox := func() int {
		res := alice.get("/api/mentions")
		wantStatus(t, "inbox", res, http.StatusOK)
		var page struct {
			Messages []struct {
				UserID uuid.UUID `json:"user_id"`
			} `json:"messages"`
		}
		res.decode(t, &page)
		return len(page.Messages)
	}
	if n := inbox(); n != 3 {
		t.Fatalf("before blocking the inbox has %d entries, want 3", n)
	}
	if got := alice.readState(t, ch).MentionCount; got != 3 {
		t.Fatalf("before blocking the mention count is %d, want 3", got)
	}

	wantStatus(t, "block", alice.put("/api/me/blocks/"+bob.user.ID.String(), nil), http.StatusNoContent)
	if n := inbox(); n != 1 {
		t.Errorf("after blocking the inbox has %d entries, want carol's only", n)
	}
	if got := alice.readState(t, ch).MentionCount; got != 1 {
		t.Errorf("after blocking the mention count is %d, want 1", got)
	}
	// The blocked member's own view does not change: same statuses, same data.
	wantStatus(t, "bob still posts", bob.post("/api/channels/"+ch.String()+"/messages", map[string]any{"content": "still here @alice"}), http.StatusCreated)
	wantStatus(t, "bob still reads", bob.get("/api/channels/"+ch.String()+"/messages"), http.StatusOK)
	wantStatus(t, "bob still reacts", bob.post("/api/messages/"+mine.String()+"/reactions", map[string]string{"emoji": "👍"}), http.StatusOK)
	if n := inbox(); n != 1 {
		t.Errorf("a new mention by the blocked member reached the inbox: %d", n)
	}

	wantStatus(t, "unblock", alice.delete("/api/me/blocks/"+bob.user.ID.String()), http.StatusNoContent)
	if n := inbox(); n != 4 {
		t.Errorf("after unblocking the earlier mentions are back: %d, want 4", n)
	}
}
