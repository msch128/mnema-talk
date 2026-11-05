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
