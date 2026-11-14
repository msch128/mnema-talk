package messages

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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
	_, _, err := editMessage(ctx, p, online, messageID, userID, textEdit{content: &content}, nil)
	return err
}

// editMessage is EditMessage returning the edited message, read in the
// edit's transaction: an error means that nothing was changed. audited, if
// set, runs in that transaction after the new text is stored (the audit
// entry). It also returns the members the message mentioned before the edit.
func editMessage(ctx context.Context, p *db.Pool, online mentions.OnlineSource, messageID, userID uuid.UUID, edit textEdit, audited func(pgx.Tx) error) (*core.Message, []uuid.UUID, error) {
	pending, err := checkEditAutomod(ctx, p, messageID, userID, edit.text())
	if err != nil {
		return nil, nil, err
	}
	var msg *core.Message
	var before []uuid.UUID
	err = pgx.BeginFunc(ctx, p, func(tx pgx.Tx) error {
		var err error
		if before, err = mentionedBy(ctx, tx, messageID); err != nil {
			return err
		}
		channel, err := editTx(ctx, tx, online, messageID, userID, edit)
		if err != nil {
			return err
		}
		if audited != nil {
			if err := audited(tx); err != nil {
				return err
			}
		}
		if msg, err = recordMessageEvent(ctx, p, tx, events.MessageUpdate, channel, messageID, nil); err != nil || msg != nil {
			return err
		}
		msg, err = loadStored(ctx, tx, messageID, nil)
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	pending.Commit(ctx, messageID)
	return msg, before, nil
}

// EditMessageTx checks that userID may edit the message, keeps the replaced
// version and stores the new text with its mentions. It returns the message's
// channel.
func EditMessageTx(ctx context.Context, tx pgx.Tx, online mentions.OnlineSource, messageID, userID uuid.UUID, content string) (uuid.UUID, error) {
	return editTx(ctx, tx, online, messageID, userID, textEdit{content: &content})
}

func editTx(ctx context.Context, tx pgx.Tx, online mentions.OnlineSource, messageID, userID uuid.UUID, edit textEdit) (uuid.UUID, error) {
	var author, channel uuid.UUID
	var hasMedia, isPoll, forwarded bool
	err := tx.QueryRow(ctx, `
		SELECT user_id, channel_id, EXISTS (SELECT 1 FROM media WHERE message_id = messages.id),
		       EXISTS (SELECT 1 FROM polls WHERE message_id = messages.id), forwarded
		FROM messages WHERE id = $1 AND (expires_at IS NULL OR expires_at > now()) FOR NO KEY UPDATE`, messageID).Scan(&author, &channel, &hasMedia, &isPoll, &forwarded)
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
	if edit.content == nil {
		return channel, nil
	}
	content := *edit.content
	if content == "" && !hasMedia && !forwarded && !edit.mayBeEmpty {
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
	var reactions []core.ReactionSummary
	err := pgx.BeginFunc(ctx, p, func(tx pgx.Tx) (err error) {
		if changed, err = applyReaction(ctx, p, tx, messageID, userID, emoji, canAdd, target); err != nil {
			return err
		}
		// Read in the transaction: an error means that nothing changed, so
		// a retry of the toggle does not undo a change that answered 500.
		reactions, err = ReactionSummary(ctx, tx, messageID)
		return err
	})
	if err != nil {
		return nil, false, err
	}
	return reactions, changed, nil
}

// applyReaction is changeReaction's change in tx; it says whether there was one.
func applyReaction(ctx context.Context, p *db.Pool, tx pgx.Tx, messageID, userID uuid.UUID, emoji string, canAdd bool, target func(present bool) bool) (bool, error) {
	// The row lock serializes toggles on one message, so concurrent
	// adds cannot overshoot the limits. NO KEY UPDATE still lets replies,
	// uploads and quotes (FOR KEY SHARE through their foreign keys) in.
	channel, err := lockMessage(ctx, tx, messageID)
	if err != nil {
		return false, err
	}
	present, err := reactionPresent(ctx, tx, messageID, userID, emoji)
	if err != nil || target(present) == present {
		return false, err
	}
	if present {
		err = removeReaction(ctx, tx, messageID, userID, emoji)
	} else {
		err = addReactionAs(ctx, tx, messageID, userID, emoji, canAdd)
	}
	if err != nil {
		return false, err
	}
	return true, recordReactions(ctx, p, tx, channel, messageID)
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
	err := tx.QueryRow(ctx, `SELECT channel_id FROM messages WHERE id = $1 AND (expires_at IS NULL OR expires_at > now()) FOR NO KEY UPDATE`, messageID).Scan(&channel)
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
