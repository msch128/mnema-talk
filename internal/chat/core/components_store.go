package core

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/mnema-xyz/talk/internal/db"
)

// SaveComponents stores the validated components of a bot's message
// (message_components, migration 0105), replacing what it had. No
// components removes the row. It runs in the transaction that stores or
// edits the message, so the message event already carries them.
func SaveComponents(ctx context.Context, q db.Querier, messageID uuid.UUID, comps []ActionRow) error {
	if len(comps) == 0 {
		if _, err := q.Exec(ctx, `DELETE FROM message_components WHERE message_id = $1`, messageID); err != nil {
			return fmt.Errorf("remove components: %w", err)
		}
		return nil
	}
	raw, err := json.Marshal(comps)
	if err != nil {
		return fmt.Errorf("marshal components: %w", err)
	}
	if _, err := q.Exec(ctx, `
		INSERT INTO message_components (message_id, components) VALUES ($1, $2)
		ON CONFLICT (message_id) DO UPDATE SET components = EXCLUDED.components, updated_at = NOW()`, messageID, raw); err != nil {
		return fmt.Errorf("store components: %w", err)
	}
	return nil
}

// QueueComponents queues the query that fills msgs[].Components; idx maps a
// message ID to its position in msgs. It is one of the loaders of
// messages.Enrich.
func QueueComponents(_ context.Context, b *pgx.Batch, msgs []Message, idx map[uuid.UUID]int, ids []uuid.UUID) {
	b.Queue(`SELECT message_id, components FROM message_components WHERE message_id = ANY($1)`, ids).
		Query(func(rows pgx.Rows) error {
			if err := scanComponents(rows, msgs, idx); err != nil {
				return fmt.Errorf("load components: %w", err)
			}
			return nil
		})
}

func scanComponents(rows pgx.Rows, msgs []Message, idx map[uuid.UUID]int) error {
	for rows.Next() {
		var msgID uuid.UUID
		var raw []byte
		if err := rows.Scan(&msgID, &raw); err != nil {
			return err
		}
		var comps []ActionRow
		if err := json.Unmarshal(raw, &comps); err != nil {
			return fmt.Errorf("decode components: %w", err)
		}
		msgs[idx[msgID]].Components = comps
	}
	return rows.Err()
}
