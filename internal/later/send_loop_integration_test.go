//go:build integration

package later

import (
	"context"
	"testing"
	"time"
)

func TestAMessageEditedWhileItIsBeingSentIsNotSentInItsOldVersion(t *testing.T) {
	f := newFixture(t)
	s := f.service(f.p)
	ctx := context.Background()
	e, err := s.AddScheduled(ctx, NewScheduled{User: f.user, Channel: f.channel, Content: "old", SendAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	stale := dueScheduled{id: e.ID, user: f.user, channel: f.channel, content: "old", updatedAt: e.UpdatedAt.Add(-time.Second)}
	s.sendOne(ctx, stale)
	s.fail(ctx, stale, ReasonRefused) // also a refusal of the old version changes nothing

	var n, state int
	if err := f.p.QueryRow(ctx, `SELECT (SELECT COUNT(*) FROM messages), (SELECT COUNT(*) FROM scheduled_messages WHERE state = 'pending')`).Scan(&n, &state); err != nil {
		t.Fatal(err)
	}
	if n != 0 || state != 1 {
		t.Fatalf("messages %d, pending %d: the superseded version was sent or failed", n, state)
	}
}

func TestAnEndedContextStopsTheSendLoop(t *testing.T) {
	f := newFixture(t)
	s := f.service(f.p)
	if _, err := f.p.Exec(context.Background(), `INSERT INTO scheduled_messages (user_id, channel_id, content, send_at) VALUES ($1, $2, 'x', NOW() - INTERVAL '1 minute')`, f.user, f.channel); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s.sendScheduled(ctx)
	var n int
	if err := f.p.QueryRow(context.Background(), `SELECT COUNT(*) FROM scheduled_messages`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("rows %d, %v", n, err)
	}
}
