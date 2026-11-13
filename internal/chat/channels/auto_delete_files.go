package channels

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/chat/messages"
)

// purgeFilesBatch removes up to autoDeleteBatch files of the channel's
// messages older than the cutoff, for the scope "attachments": the rows stay,
// marked deleted, so the messages keep their text and show "File removed"
// where each file was. Files of pinned messages stay. It returns 0 messages
// and the number of files.
//
// The media rows are locked first, and their messages with a share lock that
// holds off a pin until the commit; the pin is then read again in a statement
// of its own, after the locks are held.
func (a *AutoDeleter) purgeFilesBatch(ctx context.Context, c autoDeleteChannel, cutoff time.Time) (int, int, error) {
	var keys []string
	var touched []uuid.UUID
	err := pgx.BeginFunc(ctx, a.DB, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT md.id FROM media md JOIN messages m ON m.id = md.message_id
			WHERE m.channel_id = $1 AND m.created_at < $2 AND NOT m.is_pinned AND NOT md.is_deleted
			  AND `+stillSetSQL("m")+`
			ORDER BY m.created_at
			LIMIT $5
			FOR UPDATE OF md SKIP LOCKED FOR SHARE OF m SKIP LOCKED`, c.id, cutoff, c.hours, c.scope, autoDeleteBatch)
		if err != nil {
			return fmt.Errorf("select expired files: %w", err)
		}
		ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		if err != nil || len(ids) == 0 {
			return err
		}
		keys, touched, err = markFilesRemoved(ctx, tx, ids)
		return err
	})
	if err != nil {
		return 0, 0, err
	}
	core.DeleteObjects(ctx, a.Objects, keys)
	a.announceFilesRemoved(ctx, touched)
	return 0, len(keys), nil
}

// markFilesRemoved marks the locked files deleted unless their message was
// pinned meanwhile, and returns their storage keys and their messages.
func markFilesRemoved(ctx context.Context, tx pgx.Tx, ids []uuid.UUID) ([]string, []uuid.UUID, error) {
	rows, err := tx.Query(ctx, `
		UPDATE media md SET is_deleted = TRUE FROM messages m
		WHERE md.id = ANY($1) AND m.id = md.message_id AND NOT m.is_pinned AND NOT md.is_deleted
		RETURNING md.s3_key, md.message_id`, ids)
	if err != nil {
		return nil, nil, fmt.Errorf("mark files removed: %w", err)
	}
	defer rows.Close()
	var keys []string
	var touched []uuid.UUID
	seen := map[uuid.UUID]bool{}
	for rows.Next() {
		var key string
		var msg uuid.UUID
		if err := rows.Scan(&key, &msg); err != nil {
			return nil, nil, fmt.Errorf("mark files removed: %w", err)
		}
		keys = append(keys, key)
		if !seen[msg] {
			seen[msg] = true
			touched = append(touched, msg)
		}
	}
	return keys, touched, rows.Err()
}

// announceFilesRemoved sends message_update for each message that lost its
// files, so open clients draw the placeholder at once. A message that cannot
// be read now (deleted meanwhile) is skipped.
func (a *AutoDeleter) announceFilesRemoved(ctx context.Context, touched []uuid.UUID) {
	if a.Events == nil {
		return
	}
	for _, id := range touched {
		msg, err := messages.GetMessage(ctx, a.DB, id)
		if err != nil {
			slog.Debug("auto-delete: reload message", "message", id, "err", err)
			continue
		}
		a.Events.BroadcastChannel(msg.ChannelID, (*core.MessageUpdate)(msg))
	}
}
