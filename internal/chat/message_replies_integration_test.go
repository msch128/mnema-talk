//go:build integration

package chat

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func TestBulkDeleteKeepsRepliesOfOtherMembersInTheAuditTrail(t *testing.T) {
	f := newChatFixture(t)
	ctx := context.Background()
	var other uuid.UUID
	if err := f.p.QueryRow(ctx, `INSERT INTO users (username, display_name, password_hash) VALUES ('replier','Replier','test-only-unused') RETURNING id`).Scan(&other); err != nil {
		t.Fatal(err)
	}
	reply, err := CreateMessage(ctx, f.p, NewMessage{ChannelID: f.c.ID, UserID: other, Content: "reply", ParentID: &f.m})
	if err != nil {
		t.Fatal(err)
	}
	var gone []deletedMessage
	err = pgx.BeginFunc(ctx, f.p, func(tx pgx.Tx) error {
		var err error
		gone, _, err = takeLatestMessages(ctx, tx, f.c.ID, f.u.ID, 10, true)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(gone) != 1 || len(gone[0].replyIDs) != 1 || gone[0].replyIDs[0] != reply {
		t.Fatalf("deleted = %+v, want the root with reply %s", gone, reply)
	}
}

func TestMessageTimestampIsTakenAtInsert(t *testing.T) {
	f := newChatFixture(t)
	ctx := context.Background()
	var created, started time.Time
	err := pgx.BeginFunc(ctx, f.p, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT NOW()`).Scan(&started); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `SELECT pg_sleep(0.05)`); err != nil {
			return err
		}
		id, err := CreateMessage(ctx, tx, NewMessage{ChannelID: f.c.ID, UserID: f.u.ID, Content: "late"})
		if err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT created_at FROM messages WHERE id = $1`, id).Scan(&created)
	})
	if err != nil {
		t.Fatal(err)
	}
	if !created.After(started) {
		t.Errorf("created_at %v is not after the transaction start %v", created, started)
	}
}

func TestEditDoesNotBlockThreadReplies(t *testing.T) {
	f := newChatFixture(t)
	ctx := context.Background()
	tx, err := f.p.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := editMessageTx(ctx, tx, nil, f.m, f.u.ID, "edited"); err != nil {
		t.Fatal(err)
	}
	short, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if _, err := CreateMessage(short, f.p, NewMessage{ChannelID: f.c.ID, UserID: f.u.ID, Content: "reply", ParentID: &f.m}); err != nil {
		t.Fatalf("a reply waits for the open edit of its root: %v", err)
	}
}

func TestReplyValidationDoesNotHideDatabaseErrors(t *testing.T) {
	f := newChatFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := ValidateParent(ctx, f.p, f.c.ID, &f.m); !errors.Is(err, context.Canceled) {
		t.Errorf("ValidateParent: %v, want the context error", err)
	}
	if err := ValidateReplyTarget(ctx, f.p, f.c.ID, nil, &f.m); !errors.Is(err, context.Canceled) {
		t.Errorf("ValidateReplyTarget: %v, want the context error", err)
	}
	missing := uuid.New()
	requireChatError(t, ValidateParent(context.Background(), f.p, f.c.ID, &missing), "parent message not found")
	requireChatError(t, ValidateReplyTarget(context.Background(), f.p, f.c.ID, nil, &missing), "message to reply to not found")
}
