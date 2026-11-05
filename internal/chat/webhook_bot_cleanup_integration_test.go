//go:build integration

package chat

import (
	"context"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat/channels"
	"github.com/msch128/mnema-talk/internal/chat/messages"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/testutil/pgfault"
)

// A webhook's hidden bot account has no use once its channel is gone: the
// webhook row goes by cascade, so the account and its avatar object would be
// kept forever unless the channel delete removes them.
func TestDeletingAChannelRemovesItsWebhookBotAccount(t *testing.T) {
	f := newChatFixture(t)
	ctx := context.Background()
	ch, err := channels.CreateChannelWithStage(ctx, f.p, nil, "hooked", ChannelTypeText, "", 0, false)
	if err != nil {
		t.Fatal(err)
	}
	var bot, avatar uuid.UUID
	if err := f.p.QueryRow(ctx, `INSERT INTO users (username, display_name, password_hash, is_bot) VALUES ('wh-test', 'wh-test', 'x', TRUE) RETURNING id`).Scan(&bot); err != nil {
		t.Fatal(err)
	}
	if err := f.p.QueryRow(ctx, `INSERT INTO media (uploader_id, s3_bucket, s3_key, original_filename, mime_type, size_bytes) VALUES ($1, 'b', 'avatars/bot.png', 'a.png', 'image/png', 1) RETURNING id`, bot).Scan(&avatar); err != nil {
		t.Fatal(err)
	}
	if _, err := f.p.Exec(ctx, `UPDATE users SET avatar_s3_key = $2 WHERE id = $1`, bot, avatar.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := f.p.Exec(ctx, `INSERT INTO webhooks (channel_id, bot_user_id, name, token_hash) VALUES ($1, $2, 'hook', '\x00')`, ch.ID, bot); err != nil {
		t.Fatal(err)
	}
	if _, err := messages.CreateMessage(ctx, f.p, NewMessage{ChannelID: ch.ID, UserID: bot, Content: "from the hook"}); err != nil {
		t.Fatal(err)
	}

	keys, err := channels.DeleteChannel(ctx, f.p, ch.ID)
	if err != nil {
		t.Fatal(err)
	}
	var left int
	if err := f.p.QueryRow(ctx, `SELECT count(*) FROM users WHERE id = $1`, bot).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != 0 {
		t.Fatal("the webhook's bot account outlived its channel")
	}
	if !slices.Contains(keys, "avatars/bot.png") {
		t.Fatalf("the bot's avatar object is not handed back for removal: %v", keys)
	}
	var members int
	if err := f.p.QueryRow(ctx, `SELECT count(*) FROM users WHERE id = $1`, f.u.ID).Scan(&members); err != nil || members != 1 {
		t.Fatalf("members must stay: %d %v", members, err)
	}
}

// A failing statement while the webhook bots are cleaned up fails the whole
// channel delete and leaves the channel in place.
func TestDeletingAChannelReportsWebhookBotCleanupFailures(t *testing.T) {
	f := newChatFixture(t)
	ctx := context.Background()
	ch, err := channels.CreateChannelWithStage(ctx, f.p, nil, "hooked-faults", ChannelTypeText, "", 0, false)
	if err != nil {
		t.Fatal(err)
	}
	var bot uuid.UUID
	if err := f.p.QueryRow(ctx, `INSERT INTO users (username, display_name, password_hash, is_bot) VALUES ('wh-faults', 'wh-faults', 'x', TRUE) RETURNING id`).Scan(&bot); err != nil {
		t.Fatal(err)
	}
	if _, err := f.p.Exec(ctx, `INSERT INTO webhooks (channel_id, bot_user_id, name, token_hash) VALUES ($1, $2, 'hook', '\x00')`, ch.ID, bot); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		"SELECT bot_user_id FROM webhooks",
		"SELECT m.s3_key FROM media",
		"DELETE FROM users u",
	} {
		pool := &db.Pool{Pool: pgfault.Pool(t, f.p.Pool, &pgfault.Tracer{Query: query})}
		if _, err := channels.DeleteChannel(ctx, pool, ch.ID); err == nil {
			t.Fatalf("%q failing: the deletion reported success", query)
		}
	}
	var n int
	if err := f.p.QueryRow(ctx, `SELECT COUNT(*) FROM channels WHERE id = $1`, ch.ID).Scan(&n); err != nil || n != 1 {
		t.Fatalf("channel rows = %d, %v, want it kept", n, err)
	}
}
