package messages

import (
	"context"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/db"
)

// ChannelMediaKeys returns the storage keys of the media attached to the
// messages of the given channels.
func ChannelMediaKeys(ctx context.Context, q db.Querier, ids []uuid.UUID) ([]string, error) {
	var keys []string
	err := CollectKeys(ctx, q, &keys, `
		SELECT s3_key FROM media WHERE message_id IN
			(SELECT id FROM messages WHERE channel_id = ANY($1))`, ids)
	return keys, err
}

// DeleteEventCovers removes the cover images of the events held in the given
// channels and returns their storage keys. The channel delete cascades the
// events away but only nulls media.channel_id, so a cover row would stay
// behind, readable by every member and never cleaned up.
func DeleteEventCovers(ctx context.Context, q db.Querier, ids []uuid.UUID) ([]string, error) {
	var keys []string
	err := CollectKeys(ctx, q, &keys, `
		DELETE FROM media WHERE id IN
			(SELECT cover_media_id FROM events WHERE channel_id = ANY($1) AND cover_media_id IS NOT NULL)
		RETURNING s3_key`, ids)
	return keys, err
}
