//go:build integration

package chat

import (
	"context"
	"errors"
	"testing"

	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/chat/chattest"
	"github.com/msch128/mnema-talk/internal/chat/messages"
)

var errRollBack = errors.New("roll back")

func TestBulkDeleteSurvivesAFailingEditVersionRead(t *testing.T) {
	f := newChatFixture(t)
	ctx := context.Background()
	err := pgx.BeginFunc(ctx, f.p, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `ALTER TABLE message_edits RENAME TO message_edits_gone`); err != nil {
			return err
		}
		gone, _, err := messages.TakeLatestMessages(ctx, tx, f.c.ID, f.u.ID, 5, true)
		if err != nil || len(gone) == 0 {
			t.Fatalf("takeLatestMessages = %d messages, %v", len(gone), err)
		}
		return errRollBack // the rename must not outlive the test
	})
	if !errors.Is(err, errRollBack) {
		t.Fatal(err)
	}
}

func TestBulkDeleteSurvivesAFailingReplyRead(t *testing.T) {
	f := newChatFixture(t)
	ctx := context.Background()
	err := pgx.BeginFunc(ctx, f.p, func(tx pgx.Tx) error {
		// A column the reply read needs is gone: its statement fails and, without
		// a savepoint, takes the transaction (and the deletion) down with it.
		if _, err := tx.Exec(ctx, `ALTER TABLE messages RENAME COLUMN parent_id TO parent_id_gone`); err != nil {
			return err
		}
		if got := messages.ReplyIDsForAudit(ctx, tx, []uuid.UUID{f.m}); len(got) != 0 {
			t.Fatalf("replies = %v; want none after a failed read", got)
		}
		var one int
		if err := tx.QueryRow(ctx, `SELECT 1`).Scan(&one); err != nil {
			t.Fatalf("the transaction is unusable after the failed read: %v", err)
		}
		return errRollBack // the rename must not outlive the test
	})
	if !errors.Is(err, errRollBack) {
		t.Fatal(err)
	}
}

func TestBulkDeleteReportsDatabaseDrift(t *testing.T) {
	f := newChatFixture(t)
	ctx := context.Background()
	if _, err := f.p.Exec(ctx, `
		INSERT INTO media (uploader_id, channel_id, message_id, s3_bucket, s3_key, original_filename, mime_type, size_bytes)
		VALUES ($1, $2, $3, 'bucket', 'drift-key', 'drift.png', 'image/png', 10)`, f.u.ID, f.c.ID, f.m); err != nil {
		t.Fatal(err)
	}
	run := func(views map[string]string) error {
		p := chattest.DriftPool(t, f.p, views)
		return pgx.BeginFunc(ctx, p, func(tx pgx.Tx) error {
			_, _, err := messages.TakeLatestMessages(ctx, tx, f.c.ID, f.u.ID, 5, true)
			return err
		})
	}
	for name, tc := range map[string]struct {
		views map[string]string
		want  string
	}{
		"message query":       {map[string]string{"messages": `SELECT id FROM public.messages`}, "select messages"},
		"message decoding":    {map[string]string{"messages": `SELECT id,parent_id,channel_id,user_id,NULL::text content,created_at FROM public.messages`}, "select messages"},
		"attachment query":    {map[string]string{"media": `SELECT id FROM public.media`}, "select attachments"},
		"attachment decoding": {map[string]string{"media": `SELECT message_id,id,NULL::text original_filename,mime_type,size_bytes,s3_key,0::smallint position,NOW() created_at FROM public.media`}, ""},
		"attachment stream":   {map[string]string{"media": `SELECT message_id,id,original_filename,mime_type,1/(size_bytes-size_bytes) size_bytes,s3_key,0::smallint position,NOW() created_at FROM public.media`}, "division by zero"},
	} {
		t.Run(name, func(t *testing.T) {
			err := run(tc.views)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want containing %q", err, tc.want)
			}
		})
	}
}
