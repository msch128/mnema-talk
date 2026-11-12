package messages

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// What a delete or an edit reads in its transaction for the read-state
// recount that follows it (readstate.Recounter): who the edited message
// mentioned before, and how new the deleted messages were.

// mentionedBy lists the members a message mentions now.
func mentionedBy(ctx context.Context, tx pgx.Tx, messageID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := tx.Query(ctx, `SELECT user_id FROM message_mentions WHERE message_id = $1`, messageID)
	if err != nil {
		return nil, fmt.Errorf("mentions before the edit: %w", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, fmt.Errorf("mentions before the edit: %w", err)
	}
	return ids, nil
}

// newestOfThread is when the newest of a message and its replies was written.
func newestOfThread(ctx context.Context, tx pgx.Tx, messageID uuid.UUID) (time.Time, error) {
	var newest time.Time
	if err := tx.QueryRow(ctx, `SELECT max(created_at) FROM messages WHERE id = $1 OR parent_id = $1`, messageID).Scan(&newest); err != nil {
		return newest, fmt.Errorf("newest removed message: %w", err)
	}
	return newest, nil
}
