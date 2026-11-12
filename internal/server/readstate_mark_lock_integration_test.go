//go:build integration

package server

import (
	"context"
	"net/http"
	"testing"
	"time"
)

// A post that has drawn its number but not committed must keep a later post
// of the same channel from committing first (the channel lock in
// messages.insertMessageTx); otherwise a read-state answer taken in between
// carries the later number as its mark but not the earlier message. The first
// post is a reply that waits in its insert for the foreign-key lock on its
// thread root, which the test holds. (Written by the independent review of
// the H-L5-04 fix; random writers never hit this gap.)
func TestTheReadStateMarkHoldsWhileAnEarlierPostIsOpen(t *testing.T) {
	a := newApp(t, false)
	owner := a.seedAdmin()
	ch := a.createChannel(owner, "busy", "text")
	reader := a.register(owner, "max")
	alice := a.register(owner, "alice")
	ctx := context.Background()

	root := a.send(owner, ch, "zero")
	hold, err := a.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = hold.Rollback(ctx) }()
	if _, err := hold.Exec(ctx, `SELECT 1 FROM messages WHERE id = $1 FOR UPDATE`, root); err != nil {
		t.Fatal(err)
	}
	first := make(chan response, 1)
	go func() {
		first <- alice.post("/api/channels/"+ch.String()+"/messages", map[string]any{"content": "@max first", "parent_id": root})
	}()
	waitForLockWaiters(t, a, 1)
	second := make(chan response, 1)
	go func() { second <- owner.post("/api/channels/"+ch.String()+"/messages", map[string]any{"content": "@max second"}) }()
	// Either the second post commits (no channel lock) or it waits for the first.
	select {
	case res := <-second:
		if res.status != http.StatusCreated {
			t.Fatalf("second: %d %s", res.status, res.body)
		}
		second <- res
	case <-time.After(1500 * time.Millisecond):
	}
	s := reader.readState(t, ch)
	_ = hold.Rollback(ctx)
	if res := <-first; res.status != http.StatusCreated {
		t.Fatalf("first: %d %s", res.status, res.body)
	}
	<-second
	var below int
	if err := a.db.QueryRow(ctx, `
		SELECT count(*) FROM messages m JOIN message_mentions mm ON mm.message_id = m.id AND mm.user_id = $3
		WHERE m.channel_id = $1 AND m.number <= $2`, ch, s.LastMessageNumber, reader.user.ID).Scan(&below); err != nil {
		t.Fatal(err)
	}
	if s.MentionCount != below {
		t.Fatalf("answer counts %d mentions but %d mentioning messages are numbered up to its mark %d (one was still open)", s.MentionCount, below, s.LastMessageNumber)
	}
}

// waitForLockWaiters waits until n sessions of the test database wait for a lock.
func waitForLockWaiters(t *testing.T, a *app, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var waiting int
		if err := a.db.QueryRow(context.Background(), `
			SELECT count(*) FROM pg_stat_activity WHERE wait_event_type = 'Lock' AND datname = current_database()`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting >= n {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("no post waits for the held row")
}
