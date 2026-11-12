package messages

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/automod"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/chat/mentions"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/emojitext"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// ServeEditMessage changes the text of a message authored by userID and records
// its mentions again, in one transaction. Empty content is only allowed when
// the message carries an attachment (like an upload without a caption).
func EditMessage(ctx context.Context, p *db.Pool, online mentions.OnlineSource, messageID, userID uuid.UUID, content string) error {
	pending, err := checkEditAutomod(ctx, p, messageID, userID, content)
	if err != nil {
		return err
	}
	err = pgx.BeginFunc(ctx, p, func(tx pgx.Tx) error {
		channel, err := EditMessageTx(ctx, tx, online, messageID, userID, content)
		if err != nil {
			return err
		}
		return RecordMessageEvent(ctx, p, tx, events.MessageUpdate, channel, messageID, nil)
	})
	if err == nil {
		pending.Commit(ctx, messageID)
	}
	return err
}

// EditMessageTx checks that userID may edit the message, keeps the replaced
// version and stores the new text with its mentions. It returns the message's
// channel.
func EditMessageTx(ctx context.Context, tx pgx.Tx, online mentions.OnlineSource, messageID, userID uuid.UUID, content string) (uuid.UUID, error) {
	var author, channel uuid.UUID
	var hasMedia, isPoll, forwarded bool
	err := tx.QueryRow(ctx, `
		SELECT user_id, channel_id, EXISTS (SELECT 1 FROM media WHERE message_id = messages.id),
		       EXISTS (SELECT 1 FROM polls WHERE message_id = messages.id), forwarded
		FROM messages WHERE id = $1 FOR NO KEY UPDATE`, messageID).Scan(&author, &channel, &hasMedia, &isPoll, &forwarded)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, core.ErrMessageNotFound
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("load message: %w", err)
	}
	if author != userID {
		return uuid.Nil, httpx.ErrForbidden("only the author can edit this message")
	}
	if isPoll {
		return uuid.Nil, httpx.ErrInvalidInput("a poll cannot be edited; delete it and ask again")
	}
	if err := checkPairWritable(ctx, tx, channel, userID); err != nil {
		return uuid.Nil, err
	}
	if content == "" && !hasMedia && !forwarded {
		_, err := core.ValidateContent(content, false)
		return uuid.Nil, err
	}
	if err := recordReplacedVersion(ctx, tx, messageID); err != nil {
		return uuid.Nil, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE messages SET content = $1, is_edited = TRUE, updated_at = NOW()
		WHERE id = $2`, content, messageID); err != nil {
		return uuid.Nil, fmt.Errorf("update message: %w", err)
	}
	return channel, mentions.RecordMentions(ctx, tx, online, messageID, userID, content)
}

// checkEditAutomod applies the automod rules to the new text of an edit. Only
// the author's own edit is checked; whoever else tries gets the refusal of the
// edit itself.
func checkEditAutomod(ctx context.Context, p *db.Pool, messageID, userID uuid.UUID, content string) (*automod.Pending, error) {
	ref, err := core.LoadMessageRef(ctx, p, messageID)
	if errors.Is(err, core.ErrMessageNotFound) {
		return nil, nil // the edit itself answers 404
	}
	if err != nil {
		return nil, err
	}
	if ref.AuthorID != userID || content == "" {
		return nil, nil
	}
	return automod.Of(p).Check(ctx, automod.Post{ChannelID: ref.ChannelID, UserID: userID, Content: content, MessageID: messageID})
}

// DeleteMessageAs removes a message (and its thread replies) if user is its
// author or canManage says they may delete other members' messages (the Manage
// messages permission in the message's channel, or being the Owner). It returns
// the storage keys of the media that went with it; their rows are removed by
// cascade, the caller deletes the objects.
func DeleteMessageAs(ctx context.Context, p *db.Pool, messageID uuid.UUID, user *auth.User, canManage bool) ([]string, error) {
	var keys []string
	err := pgx.BeginFunc(ctx, p, func(tx pgx.Tx) error {
		// Locking the message first blocks a concurrent thread reply or
		// upload (their foreign keys need a share lock on it) until we are
		// done, so every attachment the cascade removes is in keys.
		m, err := lockMessageForDelete(ctx, tx, messageID)
		if err != nil {
			return err
		}
		if err := checkDeleteAuthorized(user, m.author, m.ownerContent, canManage); err != nil {
			return err
		}
		if err := CollectKeys(ctx, tx, &keys, `
			SELECT s3_key FROM media WHERE message_id IN
				(SELECT id FROM messages WHERE id = $1 OR parent_id = $1)`, messageID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM messages WHERE id = $1`, messageID); err != nil {
			return fmt.Errorf("delete message: %w", err)
		}
		return RecordMessageDelete(ctx, p, tx, m.channel, messageID, m.parent)
	})
	if err != nil {
		return nil, err
	}
	return keys, nil
}

// lockedMessage is what deleting a message needs to know about it.
type lockedMessage struct {
	author, channel uuid.UUID
	parent          *uuid.UUID
	ownerContent    bool
}

func lockMessageForDelete(ctx context.Context, tx pgx.Tx, messageID uuid.UUID) (lockedMessage, error) {
	var m lockedMessage
	// The Owner's words (the message or a reply of theirs in its thread)
	// are only the Owner's to remove.
	err := tx.QueryRow(ctx, `
		SELECT m.user_id, m.channel_id, m.parent_id,
		       EXISTS (SELECT 1 FROM users a WHERE a.id = m.user_id AND a.role = 'admin')
		    OR EXISTS (SELECT 1 FROM messages r JOIN users ru ON ru.id = r.user_id
		               WHERE r.parent_id = m.id AND ru.role = 'admin')
		FROM messages m WHERE m.id = $1 FOR UPDATE OF m`, messageID).Scan(&m.author, &m.channel, &m.parent, &m.ownerContent)
	if errors.Is(err, pgx.ErrNoRows) {
		return m, core.ErrMessageNotFound
	}
	if err != nil {
		return m, fmt.Errorf("lock message: %w", err)
	}
	return m, nil
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

// CollectKeys appends the first column of query's rows to keys.
func CollectKeys(ctx context.Context, q db.Querier, keys *[]string, query string, args ...any) error {
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

// ToggleReactionAs adds or removes userID's reaction and returns the new
// summary. canAdd says whether userID may add reactions; without it only taking
// back an existing one works (403 otherwise). Adding is refused with 409
// CONFLICT beyond MaxReactionsPerUser per user or MaxEmojiPerMessage distinct
// emoji per message.
func ToggleReactionAs(ctx context.Context, p *db.Pool, messageID, userID uuid.UUID, emoji string, canAdd bool) ([]core.ReactionSummary, error) {
	reactions, _, err := changeReaction(ctx, p, messageID, userID, emoji, canAdd, func(present bool) bool { return !present })
	return reactions, err
}

// SetReactionAs makes userID's reaction present (want) or absent, whatever it
// was: asking for the state it already has changes nothing (changed false).
// canAdd and the limits are as for ToggleReactionAs.
func SetReactionAs(ctx context.Context, p *db.Pool, messageID, userID uuid.UUID, emoji string, want, canAdd bool) (reactions []core.ReactionSummary, changed bool, err error) {
	return changeReaction(ctx, p, messageID, userID, emoji, canAdd, func(bool) bool { return want })
}

// changeReaction moves userID's reaction to the state target picks from the
// current one, under the message's row lock, which serializes all changes to
// one message so concurrent adds cannot overshoot the limits.
func changeReaction(ctx context.Context, p *db.Pool, messageID, userID uuid.UUID, emoji string, canAdd bool, target func(present bool) bool) ([]core.ReactionSummary, bool, error) {
	// One emoji with and without U+FE0F is one reaction: it is stored in one
	// form, and rows an older server stored in the other match it by key.
	emoji = core.ReactionForm(emoji)
	changed := false
	err := pgx.BeginFunc(ctx, p, func(tx pgx.Tx) error {
		// The row lock serializes toggles on one message, so concurrent
		// adds cannot overshoot the limits. NO KEY UPDATE still lets replies,
		// uploads and quotes (FOR KEY SHARE through their foreign keys) in.
		channel, err := lockMessage(ctx, tx, messageID)
		if err != nil {
			return err
		}
		present, err := reactionPresent(ctx, tx, messageID, userID, emoji)
		if err != nil || target(present) == present {
			return err
		}
		changed = true
		if present {
			err = removeReaction(ctx, tx, messageID, userID, emoji)
		} else {
			err = addReactionAs(ctx, tx, messageID, userID, emoji, canAdd)
		}
		if err != nil {
			return err
		}
		return recordReactions(ctx, p, tx, channel, messageID)
	})
	if err != nil {
		return nil, false, err
	}
	reactions, err := ReactionSummary(ctx, p, messageID)
	return reactions, changed, err
}

func reactionPresent(ctx context.Context, tx pgx.Tx, messageID, userID uuid.UUID, emoji string) (bool, error) {
	var present bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM message_reactions WHERE message_id = $1 AND user_id = $2 AND `+reactionKeySQL+` = $3)`,
		messageID, userID, core.ReactionKey(emoji)).Scan(&present); err != nil {
		return false, fmt.Errorf("read reaction: %w", err)
	}
	return present, nil
}

// removeReaction takes back userID's reaction in every form it is stored in.
func removeReaction(ctx context.Context, tx pgx.Tx, messageID, userID uuid.UUID, emoji string) error {
	if _, err := tx.Exec(ctx, `DELETE FROM message_reactions WHERE message_id = $1 AND user_id = $2 AND `+reactionKeySQL+` = $3`,
		messageID, userID, core.ReactionKey(emoji)); err != nil {
		return fmt.Errorf("remove reaction: %w", err)
	}
	return nil
}

// lockMessage takes the message's row lock (errMessageNotFound when it is
// gone) and returns its channel.
func lockMessage(ctx context.Context, tx pgx.Tx, messageID uuid.UUID) (uuid.UUID, error) {
	var channel uuid.UUID
	err := tx.QueryRow(ctx, `SELECT channel_id FROM messages WHERE id = $1 FOR NO KEY UPDATE`, messageID).Scan(&channel)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, core.ErrMessageNotFound
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("lock message: %w", err)
	}
	return channel, nil
}

// addReactionAs adds the reaction when userID may: the permission, the emoji
// and the rules of a conversation are checked first.
func addReactionAs(ctx context.Context, tx pgx.Tx, messageID, userID uuid.UUID, emoji string, canAdd bool) error {
	if !canAdd {
		return httpx.ErrForbidden("missing permission: add reactions")
	}
	if _, err := core.ValidateEmoji(emoji); err != nil {
		return err
	}
	if err := checkDMReactionAllowed(ctx, tx, messageID, userID); err != nil {
		return err
	}
	return addReaction(ctx, tx, messageID, userID, emoji)
}

func addReaction(ctx context.Context, tx pgx.Tx, messageID, userID uuid.UUID, emoji string) error {
	if err := requireKnownEmoji(ctx, tx, emoji); err != nil {
		return err
	}
	var mine, distinct int
	var emojiPresent bool
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(DISTINCT `+reactionKeySQL+`) FILTER (WHERE user_id = $2), COUNT(DISTINCT `+reactionKeySQL+`),
		       COALESCE(BOOL_OR(`+reactionKeySQL+` = $3), FALSE)
		FROM message_reactions WHERE message_id = $1`, messageID, userID, core.ReactionKey(emoji)).Scan(&mine, &distinct, &emojiPresent); err != nil {
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

// ReactionSummary loads one message's reactions, in the order they were first given.
func ReactionSummary(ctx context.Context, p db.Querier, messageID uuid.UUID) ([]core.ReactionSummary, error) {
	msgs := []core.Message{{Reactions: []core.ReactionSummary{}}}
	if err := loadReactions(ctx, p, msgs, map[uuid.UUID]int{messageID: 0}, []uuid.UUID{messageID}); err != nil {
		return nil, err
	}
	return msgs[0].Reactions, nil
}

// requireKnownEmoji refuses the token of a custom emoji that was deleted (or
// never existed); standard emoji need no record.
func requireKnownEmoji(ctx context.Context, q db.Querier, emoji string) error {
	id, ok := emojitext.ID(emoji)
	if !ok {
		return nil
	}
	var exists bool
	if err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM custom_emoji WHERE id = $1)`, id).Scan(&exists); err != nil {
		return fmt.Errorf("check emoji: %w", err)
	}
	if !exists {
		return httpx.ErrInvalidInput("invalid emoji")
	}
	return nil
}
