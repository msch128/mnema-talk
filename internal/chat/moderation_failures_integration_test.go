//go:build integration

package chat

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestBulkDeleteReportsDatabaseDrift(t *testing.T) {
	f := newChatFixture(t)
	ctx := context.Background()
	if _, err := f.p.Exec(ctx, `
		INSERT INTO media (uploader_id, channel_id, message_id, s3_bucket, s3_key, original_filename, mime_type, size_bytes)
		VALUES ($1, $2, $3, 'bucket', 'drift-key', 'drift.png', 'image/png', 10)`, f.u.ID, f.c.ID, f.m); err != nil {
		t.Fatal(err)
	}
	run := func(views map[string]string) error {
		p := driftPool(t, f.p, views)
		return pgx.BeginFunc(ctx, p, func(tx pgx.Tx) error {
			_, _, err := takeLatestMessages(ctx, tx, f.c.ID, f.u.ID, 5, true)
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

func TestSlowModeReportsDatabaseDrift(t *testing.T) {
	f := newChatFixture(t)
	ctx := context.Background()
	check := func(views map[string]string) error {
		p := driftPool(t, f.p, views)
		return pgx.BeginFunc(ctx, p, func(tx pgx.Tx) error { return gatePost(ctx, tx, NewMessage{ChannelID: f.c.ID, UserID: f.u.ID}) })
	}
	if err := check(map[string]string{"channels": `SELECT id FROM public.channels`}); err == nil || !strings.Contains(err.Error(), "load channel") {
		t.Fatalf("channel drift: %v", err)
	}
	if err := check(nil); err != nil {
		t.Fatalf("slow mode off: %v", err)
	}
	if _, err := f.p.Exec(ctx, `UPDATE channels SET slow_mode_seconds = 60 WHERE id = $1`, f.c.ID); err != nil {
		t.Fatal(err)
	}
	if err := check(map[string]string{"slow_mode_posts": `SELECT DISTINCT channel_id, user_id, last_post_at FROM public.slow_mode_posts`}); err == nil || !strings.Contains(err.Error(), "record post") {
		t.Fatalf("table drift: %v", err)
	}
	if err := check(nil); err != nil {
		t.Fatalf("first post: %v", err)
	}
	if err := check(nil); err == nil || !strings.Contains(err.Error(), "slow mode") {
		t.Fatalf("second post: %v", err)
	}
	if _, err := f.p.Exec(ctx, `DELETE FROM channels WHERE id = $1`, f.c.ID); err != nil {
		t.Fatal(err)
	}
	if err := check(nil); err != errChannelNotFound {
		t.Fatalf("missing channel: %v", err)
	}
}
