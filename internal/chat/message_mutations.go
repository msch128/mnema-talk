package chat

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// EditMessage changes the text of a message authored by userID and records
// its mentions again, in one transaction. Empty content is only allowed when
// the message carries an attachment (like an upload without a caption).
func EditMessage(ctx context.Context, p *db.Pool, online OnlineSource, messageID, userID uuid.UUID, content string) error {
	return pgx.BeginFunc(ctx, p, func(tx pgx.Tx) error {
		var author uuid.UUID
		var hasMedia, isPoll bool
		err := tx.QueryRow(ctx, `
			SELECT user_id, EXISTS (SELECT 1 FROM media WHERE message_id = messages.id),
			       EXISTS (SELECT 1 FROM polls WHERE message_id = messages.id)
			FROM messages WHERE id = $1 FOR UPDATE`, messageID).Scan(&author, &hasMedia, &isPoll)
		if errors.Is(err, pgx.ErrNoRows) {
			return errMessageNotFound
		}
		if err != nil {
			return fmt.Errorf("load message: %w", err)
		}
		if author != userID {
			return httpx.ErrForbidden("only the author can edit this message")
		}
		if isPoll {
			return httpx.ErrInvalidInput("a poll cannot be edited; delete it and ask again")
		}
		if content == "" && !hasMedia {
			_, err := ValidateContent(content, false)
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE messages SET content = $1, is_edited = TRUE, updated_at = NOW()
			WHERE id = $2`, content, messageID); err != nil {
			return fmt.Errorf("update message: %w", err)
		}
		return RecordMentions(ctx, tx, online, messageID, userID, content)
	})
}

// DeleteMessage removes a message (and its thread replies) if user is its
// author or an admin. It returns the storage keys of the media that went with
// it; their rows are removed by cascade, the caller deletes the objects.
func DeleteMessage(ctx context.Context, p *db.Pool, messageID uuid.UUID, user *auth.User) ([]string, error) {
	return DeleteMessageAs(ctx, p, messageID, user, user.IsAdmin())
}

// DeleteMessageAs is DeleteMessage where canManage says whether user may
// delete other members' messages (the Manage messages permission in the
// message's channel, or being the Owner).
func DeleteMessageAs(ctx context.Context, p *db.Pool, messageID uuid.UUID, user *auth.User, canManage bool) ([]string, error) {
	var keys []string
	err := pgx.BeginFunc(ctx, p, func(tx pgx.Tx) error {
		// Locking the message first blocks a concurrent thread reply or
		// upload (their foreign keys need a share lock on it) until we are
		// done, so every attachment the cascade removes is in keys.
		author, ownerContent, err := lockMessageForDelete(ctx, tx, messageID)
		if err != nil {
			return err
		}
		if err := checkDeleteAuthorized(user, author, ownerContent, canManage); err != nil {
			return err
		}
		if err := collectKeys(ctx, tx, &keys, `
			SELECT s3_key FROM media WHERE message_id IN
				(SELECT id FROM messages WHERE id = $1 OR parent_id = $1)`, messageID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM messages WHERE id = $1`, messageID); err != nil {
			return fmt.Errorf("delete message: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return keys, nil
}

func lockMessageForDelete(ctx context.Context, tx pgx.Tx, messageID uuid.UUID) (uuid.UUID, bool, error) {
	var author uuid.UUID
	var ownerContent bool
	// The Owner's words (the message or a reply of theirs in its thread)
	// are only the Owner's to remove.
	err := tx.QueryRow(ctx, `
		SELECT m.user_id,
		       EXISTS (SELECT 1 FROM users a WHERE a.id = m.user_id AND a.role = 'admin')
		    OR EXISTS (SELECT 1 FROM messages r JOIN users ru ON ru.id = r.user_id
		               WHERE r.parent_id = m.id AND ru.role = 'admin')
		FROM messages m WHERE m.id = $1 FOR UPDATE OF m`, messageID).Scan(&author, &ownerContent)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, false, errMessageNotFound
	}
	if err != nil {
		return uuid.Nil, false, fmt.Errorf("lock message: %w", err)
	}
	return author, ownerContent, nil
}

func checkDeleteAuthorized(user *auth.User, author uuid.UUID, ownerContent, canManage bool) error {
	if author != user.ID && !canManage {
		return httpx.ErrForbidden("only the author or a moderator can delete this message")
	}
	if ownerContent && !user.IsAdmin() && author != user.ID {
		return httpx.ErrForbidden("the Owner's messages can only be deleted by the Owner")
	}
	return nil
}

// collectKeys appends the first column of query's rows to keys.
func collectKeys(ctx context.Context, q db.Querier, keys *[]string, query string, args ...any) error {
	rows, err := q.Query(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("collect media keys: %w", err)
	}
	k, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return fmt.Errorf("collect media keys: %w", err)
	}
	*keys = append(*keys, k...)
	return nil
}

// Reaction limits: how many different emoji one user may put on a message,
// and how many different emoji a message may carry in total.
const (
	MaxReactionsPerUser    = 20
	MaxEmojiPerMessage     = 50
	errTooManyReactionsMsg = "too many reactions on this message"
)

// ToggleReaction adds or removes userID's reaction and returns the new summary.
// Adding is refused with 409 CONFLICT beyond MaxReactionsPerUser per user or
// MaxEmojiPerMessage distinct emoji per message.
func ToggleReaction(ctx context.Context, p *db.Pool, messageID, userID uuid.UUID, emoji string) ([]ReactionSummary, error) {
	return ToggleReactionAs(ctx, p, messageID, userID, emoji, true)
}

// ToggleReactionAs is ToggleReaction where canAdd says whether userID may add
// reactions; without it only taking back an existing one works (403 otherwise).
func ToggleReactionAs(ctx context.Context, p *db.Pool, messageID, userID uuid.UUID, emoji string, canAdd bool) ([]ReactionSummary, error) {
	err := pgx.BeginFunc(ctx, p, func(tx pgx.Tx) error {
		// The row lock serializes toggles on one message, so concurrent
		// adds cannot overshoot the limits.
		var one int
		err := tx.QueryRow(ctx, `SELECT 1 FROM messages WHERE id = $1 FOR UPDATE`, messageID).Scan(&one)
		if errors.Is(err, pgx.ErrNoRows) {
			return errMessageNotFound
		}
		if err != nil {
			return fmt.Errorf("lock message: %w", err)
		}
		tag, err := tx.Exec(ctx, `DELETE FROM message_reactions WHERE message_id = $1 AND user_id = $2 AND emoji = $3`, messageID, userID, emoji)
		if err != nil {
			return fmt.Errorf("remove reaction: %w", err)
		}
		if tag.RowsAffected() > 0 {
			return nil
		}
		if !canAdd {
			return httpx.ErrForbidden("missing permission: add reactions")
		}
		return addReaction(ctx, tx, messageID, userID, emoji)
	})
	if err != nil {
		return nil, err
	}
	return reactionSummary(ctx, p, messageID)
}

// addReaction adds the reaction unless the user or the message is at its limit.
func addReaction(ctx context.Context, tx pgx.Tx, messageID, userID uuid.UUID, emoji string) error {
	var mine, distinct int
	var emojiPresent bool
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(*) FILTER (WHERE user_id = $2), COUNT(DISTINCT emoji), COALESCE(BOOL_OR(emoji = $3), FALSE)
		FROM message_reactions WHERE message_id = $1`, messageID, userID, emoji).Scan(&mine, &distinct, &emojiPresent); err != nil {
		return fmt.Errorf("count reactions: %w", err)
	}
	if mine >= MaxReactionsPerUser || (!emojiPresent && distinct >= MaxEmojiPerMessage) {
		return httpx.ErrConflict(errTooManyReactionsMsg)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO message_reactions (message_id, user_id, emoji) VALUES ($1, $2, $3)
		ON CONFLICT (message_id, user_id, emoji) DO NOTHING`, messageID, userID, emoji); err != nil {
		return fmt.Errorf("add reaction: %w", err)
	}
	return nil
}

// reactionSummary loads one message's reactions, in the order they were first given.
func reactionSummary(ctx context.Context, p *db.Pool, messageID uuid.UUID) ([]ReactionSummary, error) {
	rows, err := p.Query(ctx, `
		SELECT emoji, COUNT(*), array_agg(user_id ORDER BY created_at)
		FROM message_reactions WHERE message_id = $1
		GROUP BY emoji ORDER BY MIN(created_at)`, messageID)
	if err != nil {
		return nil, fmt.Errorf("load reactions: %w", err)
	}
	defer rows.Close()
	out := make([]ReactionSummary, 0)
	for rows.Next() {
		var r ReactionSummary
		if err := rows.Scan(&r.Emoji, &r.Count, &r.Users); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
