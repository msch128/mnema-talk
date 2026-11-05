package chat

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// deleteOrphanBotAccounts removes the webhook bot accounts that no longer have
// a webhook or any message (a webhook posts into its own channel only, so after
// that channel is gone none is left) and returns their avatar storage keys: the
// media rows go with the account.
func deleteOrphanBotAccounts(ctx context.Context, tx pgx.Tx, botIDs []uuid.UUID) ([]string, error) {
	if len(botIDs) == 0 {
		return nil, nil
	}
	var keys []string
	err := collectKeys(ctx, tx, &keys, `
		SELECT m.s3_key FROM media m JOIN users u ON u.avatar_s3_key = m.id::text
		WHERE u.id = ANY($1) AND u.is_bot AND NOT m.is_deleted
		  AND NOT EXISTS (SELECT 1 FROM messages x WHERE x.user_id = u.id)`, botIDs)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM users u WHERE u.id = ANY($1) AND u.is_bot
		  AND NOT EXISTS (SELECT 1 FROM webhooks w WHERE w.bot_user_id = u.id)
		  AND NOT EXISTS (SELECT 1 FROM messages m WHERE m.user_id = u.id)`, botIDs); err != nil {
		return nil, fmt.Errorf("delete webhook bot accounts: %w", err)
	}
	return keys, nil
}
