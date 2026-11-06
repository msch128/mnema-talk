//go:build integration

package channels

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/testutil"
)

// Deleting a channel takes the cover images of its events with it: the media
// row (which is what GET /api/media/{id} answers 404 for) is gone and the
// object is handed back for deletion, instead of lingering channel-less and
// readable by every member.
func TestDeletingAChannelRemovesTheCoversOfItsEvents(t *testing.T) {
	p := testutil.DB(t)
	testutil.Reset(t, p)
	ctx := context.Background()
	ch, err := CreateChannelWithStage(ctx, p, nil, "plans", core.ChannelTypeText, "", 0, false)
	if err != nil {
		t.Fatal(err)
	}
	var uid, mediaID uuid.UUID
	if err := p.QueryRow(ctx, `INSERT INTO users (username, display_name, password_hash, role) VALUES ('u', 'u', 'x', 'user') RETURNING id`).Scan(&uid); err != nil {
		t.Fatal(err)
	}
	if err := p.QueryRow(ctx, `
		INSERT INTO media (uploader_id, channel_id, s3_bucket, s3_key, original_filename, mime_type, size_bytes)
		VALUES ($1, $2, 'b', 'covers/x.png', 'cover.png', 'image/png', 3) RETURNING id`, uid, ch.ID).Scan(&mediaID); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Exec(ctx, `INSERT INTO events (title, start_time, channel_id, cover_media_id) VALUES ('Raid', NOW() + interval '1 day', $1, $2)`, ch.ID, mediaID); err != nil {
		t.Fatal(err)
	}

	keys, err := DeleteChannel(ctx, p, ch.ID)
	if err != nil {
		t.Fatal(err)
	}

	if len(keys) != 1 || keys[0] != "covers/x.png" {
		t.Errorf("keys to delete = %v, want the cover", keys)
	}
	var n int
	if err := p.QueryRow(ctx, `SELECT count(*) FROM media WHERE id = $1`, mediaID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Error("the cover's media row survived the channel delete")
	}
}
