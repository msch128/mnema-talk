package chat

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

const (
	MaxMessageLen = 4000
	maxEmojiBytes = 32
)

type MediaAttachment struct {
	ID               uuid.UUID `json:"id"`
	OriginalFilename string    `json:"original_filename"`
	MimeType         string    `json:"mime_type"`
	SizeBytes        int64     `json:"size_bytes"`
	URL              string    `json:"url"`
	IsDeleted        bool      `json:"is_deleted"`
}

type ReactionSummary struct {
	Emoji string      `json:"emoji"`
	Count int         `json:"count"`
	Users []uuid.UUID `json:"users"`
}

type Message struct {
	ID          uuid.UUID         `json:"id"`
	ChannelID   uuid.UUID         `json:"channel_id"`
	UserID      uuid.UUID         `json:"user_id"`
	ParentID    *uuid.UUID        `json:"parent_id,omitempty"`
	Username    string            `json:"username"`
	DisplayName string            `json:"display_name"`
	AvatarURL   string            `json:"avatar_url,omitempty"`
	Content     string            `json:"content"`
	IsPinned    bool              `json:"is_pinned"`
	IsEdited    bool              `json:"is_edited"`
	ReplyCount  int               `json:"reply_count"`
	ReplyToID   *uuid.UUID        `json:"reply_to_id,omitempty"`
	ReplyTo     *ReplyPreview     `json:"reply_to,omitempty"`
	Attachments []MediaAttachment `json:"attachments"`
	Reactions   []ReactionSummary `json:"reactions"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

// ReplyPreview is the quoted header of a reply. Deleted is true when the
// original message no longer exists.
type ReplyPreview struct {
	ID             uuid.UUID `json:"id"`
	Deleted        bool      `json:"deleted"`
	UserID         uuid.UUID `json:"user_id,omitempty"`
	Username       string    `json:"username,omitempty"`
	DisplayName    string    `json:"display_name,omitempty"`
	AvatarURL      string    `json:"avatar_url,omitempty"`
	Content        string    `json:"content,omitempty"`
	HasAttachments bool      `json:"has_attachments,omitempty"`
}

// replyPreviewLen bounds the quoted snippet sent with every reply.
const replyPreviewLen = 200

// ValidateContent trims and length-checks message text. Empty content is only
// allowed when the message carries an attachment.
func ValidateContent(content string, allowEmpty bool) (string, error) {
	return httpx.CleanText("content", content, MaxMessageLen, !allowEmpty)
}

// ValidateEmoji accepts a short, printable, whitespace-free reaction token.
func ValidateEmoji(emoji string) (string, error) {
	emoji = strings.TrimSpace(emoji)
	if emoji == "" || len(emoji) > maxEmojiBytes || !utf8.ValidString(emoji) {
		return "", httpx.ErrInvalidInput("invalid emoji")
	}
	for _, r := range emoji {
		if unicode.IsSpace(r) || unicode.IsControl(r) || r == '<' || r == '>' {
			return "", httpx.ErrInvalidInput("invalid emoji")
		}
	}
	return emoji, nil
}

// messageSelect loads messages with author, reply count and the quoted reply
// preview in one round trip. rm/ru are the replied-to message and its author.
const messageSelect = `
	SELECT m.id, m.channel_id, m.user_id, m.parent_id, u.username, u.display_name, u.avatar_s3_key,
	       m.content, m.is_pinned, m.is_edited,
	       (SELECT COUNT(*) FROM messages r WHERE r.parent_id = m.id),
	       m.created_at, m.updated_at,
	       m.reply_to_id, rm.id, rm.user_id, ru.username, ru.display_name, ru.avatar_s3_key,
	       LEFT(rm.content, ` + "200" + `),
	       CASE WHEN rm.id IS NULL THEN FALSE
	            ELSE EXISTS (SELECT 1 FROM media md WHERE md.message_id = rm.id) END
	FROM messages m
	JOIN users u ON u.id = m.user_id
	LEFT JOIN messages rm ON rm.id = m.reply_to_id
	LEFT JOIN users ru ON ru.id = rm.user_id`

func scanMessages(rows pgx.Rows) ([]Message, error) {
	defer rows.Close()
	msgs := make([]Message, 0)
	for rows.Next() {
		m := Message{Attachments: make([]MediaAttachment, 0), Reactions: make([]ReactionSummary, 0)}
		var avatar, rUsername, rDisplay, rAvatar, rContent *string
		var rID, rUser *uuid.UUID
		var rHasMedia bool
		if err := rows.Scan(&m.ID, &m.ChannelID, &m.UserID, &m.ParentID, &m.Username, &m.DisplayName, &avatar,
			&m.Content, &m.IsPinned, &m.IsEdited, &m.ReplyCount, &m.CreatedAt, &m.UpdatedAt,
			&m.ReplyToID, &rID, &rUser, &rUsername, &rDisplay, &rAvatar, &rContent, &rHasMedia); err != nil {
			return nil, err
		}
		m.AvatarURL = auth.AvatarURL(avatar)
		if m.ReplyToID != nil {
			p := &ReplyPreview{ID: *m.ReplyToID, Deleted: rID == nil}
			if rID != nil {
				p.UserID = *rUser
				p.Username = deref(rUsername)
				p.DisplayName = deref(rDisplay)
				p.AvatarURL = auth.AvatarURL(rAvatar)
				p.Content = deref(rContent)
				p.HasAttachments = rHasMedia
			}
			m.ReplyTo = p
		}
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// enrich loads attachments and reactions for msgs in two queries.
func enrich(ctx context.Context, p *db.Pool, msgs []Message) error {
	if len(msgs) == 0 {
		return nil
	}
	idx := make(map[uuid.UUID]int, len(msgs))
	ids := make([]uuid.UUID, len(msgs))
	for i, m := range msgs {
		idx[m.ID] = i
		ids[i] = m.ID
	}

	rows, err := p.Query(ctx, `
		SELECT id, message_id, original_filename, mime_type, size_bytes, is_deleted
		FROM media WHERE message_id = ANY($1) ORDER BY created_at`, ids)
	if err != nil {
		return fmt.Errorf("load attachments: %w", err)
	}
	for rows.Next() {
		var a MediaAttachment
		var msgID uuid.UUID
		if err := rows.Scan(&a.ID, &msgID, &a.OriginalFilename, &a.MimeType, &a.SizeBytes, &a.IsDeleted); err != nil {
			rows.Close()
			return err
		}
		a.URL = "/api/media/" + a.ID.String()
		msgs[idx[msgID]].Attachments = append(msgs[idx[msgID]].Attachments, a)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	rows, err = p.Query(ctx, `
		SELECT message_id, emoji, COUNT(*), array_agg(user_id ORDER BY created_at)
		FROM message_reactions WHERE message_id = ANY($1)
		GROUP BY message_id, emoji ORDER BY MIN(created_at)`, ids)
	if err != nil {
		return fmt.Errorf("load reactions: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var r ReactionSummary
		var msgID uuid.UUID
		if err := rows.Scan(&msgID, &r.Emoji, &r.Count, &r.Users); err != nil {
			return err
		}
		msgs[idx[msgID]].Reactions = append(msgs[idx[msgID]].Reactions, r)
	}
	return rows.Err()
}

func queryMessages(ctx context.Context, p *db.Pool, sql string, args ...any) ([]Message, error) {
	rows, err := p.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("query messages: %w", err)
	}
	msgs, err := scanMessages(rows)
	if err != nil {
		return nil, err
	}
	return msgs, enrich(ctx, p, msgs)
}

// HistoryQuery selects a page of a channel's root messages. At most one anchor
// is set: Before/After page relative to a message (exclusive), Around returns
// the anchor with messages on both sides (for jump-to-message). No anchor
// returns the newest page.
type HistoryQuery struct {
	Limit  int
	Before *uuid.UUID
	After  *uuid.UUID
	Around *uuid.UUID
}

// rootCursor is the (created_at, id) position of an anchor message; the id
// breaks ties between messages created in the same microsecond.
const rootCursor = `(SELECT created_at, id FROM messages WHERE id = $2)`

// GetChannelMessages returns a page of root messages, always oldest first.
func GetChannelMessages(ctx context.Context, p *db.Pool, channelID uuid.UUID, q HistoryQuery) ([]Message, error) {
	switch {
	case q.Before != nil:
		return olderThan(ctx, p, channelID, *q.Before, q.Limit, false)
	case q.After != nil:
		return newerThan(ctx, p, channelID, *q.After, q.Limit)
	case q.Around != nil:
		// Half the page before the anchor (including it), the rest after.
		older, err := olderThan(ctx, p, channelID, *q.Around, q.Limit/2+1, true)
		if err != nil {
			return nil, err
		}
		newer, err := newerThan(ctx, p, channelID, *q.Around, q.Limit-len(older))
		if err != nil {
			return nil, err
		}
		return append(older, newer...), nil
	}
	msgs, err := queryMessages(ctx, p, messageSelect+`
		WHERE m.channel_id = $1 AND m.parent_id IS NULL
		ORDER BY m.created_at DESC, m.id DESC
		LIMIT $2`, channelID, q.Limit)
	if err != nil {
		return nil, err
	}
	reverse(msgs)
	return msgs, nil
}

func olderThan(ctx context.Context, p *db.Pool, channelID, anchor uuid.UUID, limit int, inclusive bool) ([]Message, error) {
	op := "<"
	if inclusive {
		op = "<="
	}
	msgs, err := queryMessages(ctx, p, messageSelect+`
		WHERE m.channel_id = $1 AND m.parent_id IS NULL AND (m.created_at, m.id) `+op+` `+rootCursor+`
		ORDER BY m.created_at DESC, m.id DESC
		LIMIT $3`, channelID, anchor, limit)
	if err != nil {
		return nil, err
	}
	reverse(msgs)
	return msgs, nil
}

func newerThan(ctx context.Context, p *db.Pool, channelID, anchor uuid.UUID, limit int) ([]Message, error) {
	if limit <= 0 {
		return []Message{}, nil
	}
	return queryMessages(ctx, p, messageSelect+`
		WHERE m.channel_id = $1 AND m.parent_id IS NULL AND (m.created_at, m.id) > `+rootCursor+`
		ORDER BY m.created_at ASC, m.id ASC
		LIMIT $3`, channelID, anchor, limit)
}

func reverse(msgs []Message) {
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
}

// ValidateReplyTarget checks that replyTo exists in the same channel and the
// same thread context (both root messages, or both replies of one thread).
func ValidateReplyTarget(ctx context.Context, p *db.Pool, channelID uuid.UUID, parentID, replyTo *uuid.UUID) error {
	if replyTo == nil {
		return nil
	}
	ref, err := loadMessageRef(ctx, p, *replyTo)
	if err != nil {
		return httpx.ErrInvalidInput("message to reply to not found")
	}
	sameThread := (ref.ParentID == nil && parentID == nil) ||
		(ref.ParentID != nil && parentID != nil && *ref.ParentID == *parentID) ||
		(ref.ParentID == nil && parentID != nil && *replyTo == *parentID)
	if ref.ChannelID != channelID || !sameThread {
		return httpx.ErrInvalidInput("can only reply to a message in the same conversation")
	}
	return nil
}

// GetThreadReplies returns a page of replies of a root message, oldest first.
func GetThreadReplies(ctx context.Context, p *db.Pool, parentID uuid.UUID, q HistoryQuery) ([]Message, error) {
	if q.Limit <= 0 {
		q.Limit = 50
	}
	if q.Before != nil {
		msgs, err := queryMessages(ctx, p, messageSelect+`
			WHERE m.parent_id = $1 AND (m.created_at, m.id) < `+rootCursor+`
			ORDER BY m.created_at DESC, m.id DESC
			LIMIT $3`, parentID, *q.Before, q.Limit)
		if err != nil {
			return nil, err
		}
		reverse(msgs)
		return msgs, nil
	}
	if q.After != nil {
		return queryMessages(ctx, p, messageSelect+`
			WHERE m.parent_id = $1 AND (m.created_at, m.id) > `+rootCursor+`
			ORDER BY m.created_at ASC, m.id ASC
			LIMIT $3`, parentID, *q.After, q.Limit)
	}
	return queryMessages(ctx, p, messageSelect+`
		WHERE m.parent_id = $1
		ORDER BY m.created_at ASC, m.id ASC
		LIMIT $2`, parentID, q.Limit)
}

// GetMessage returns one fully populated message.
func GetMessage(ctx context.Context, p *db.Pool, messageID uuid.UUID) (*Message, error) {
	msgs, err := queryMessages(ctx, p, messageSelect+` WHERE m.id = $1`, messageID)
	if err != nil {
		return nil, err
	}
	if len(msgs) == 0 {
		return nil, errMessageNotFound
	}
	return &msgs[0], nil
}

// NewMessage describes a message to insert. Callers validate content, parent
// and reply target beforehand.
type NewMessage struct {
	ChannelID uuid.UUID
	UserID    uuid.UUID
	Content   string
	ParentID  *uuid.UUID // thread root, for thread replies
	ReplyToID *uuid.UUID // quoted message, for Discord-style replies
}

// CreateMessage inserts a message and returns its ID.
func CreateMessage(ctx context.Context, q db.Querier, nm NewMessage) (uuid.UUID, error) {
	var id uuid.UUID
	err := q.QueryRow(ctx, `
		INSERT INTO messages (channel_id, user_id, content, parent_id, reply_to_id) VALUES ($1, $2, $3, $4, $5)
		RETURNING id`, nm.ChannelID, nm.UserID, nm.Content, nm.ParentID, nm.ReplyToID).Scan(&id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("insert message: %w", err)
	}
	return id, nil
}

// EditMessage changes the text of a message authored by userID.
func EditMessage(ctx context.Context, p *db.Pool, messageID, userID uuid.UUID, content string) error {
	tag, err := p.Exec(ctx, `
		UPDATE messages SET content = $1, is_edited = TRUE, updated_at = NOW()
		WHERE id = $2 AND user_id = $3`, content, messageID, userID)
	if err != nil {
		return fmt.Errorf("update message: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return httpx.ErrForbidden("only the author can edit this message")
	}
	return nil
}

// DeleteMessage removes a message (and its thread replies) if user is its
// author or an admin. It returns the storage keys of the media that went with
// it; their rows are removed by cascade, the caller deletes the objects.
func DeleteMessage(ctx context.Context, p *db.Pool, messageID uuid.UUID, user *auth.User) ([]string, error) {
	var keys []string
	err := pgx.BeginFunc(ctx, p, func(tx pgx.Tx) error {
		// Collected before the delete, inside the same transaction.
		if err := collectKeys(ctx, tx, &keys, `
			SELECT s3_key FROM media WHERE message_id IN
				(SELECT id FROM messages WHERE id = $1 OR parent_id = $1)`, messageID); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `DELETE FROM messages WHERE id = $1 AND (user_id = $2 OR $3)`, messageID, user.ID, user.IsAdmin())
		if err != nil {
			return fmt.Errorf("delete message: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return httpx.ErrForbidden("only the author or an admin can delete this message")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return keys, nil
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

// ToggleReaction adds or removes userID's reaction and returns the new summary.
func ToggleReaction(ctx context.Context, p *db.Pool, messageID, userID uuid.UUID, emoji string) ([]ReactionSummary, error) {
	tag, err := p.Exec(ctx, `DELETE FROM message_reactions WHERE message_id = $1 AND user_id = $2 AND emoji = $3`, messageID, userID, emoji)
	if err != nil {
		return nil, fmt.Errorf("remove reaction: %w", err)
	}
	if tag.RowsAffected() == 0 {
		if _, err := p.Exec(ctx, `
			INSERT INTO message_reactions (message_id, user_id, emoji) VALUES ($1, $2, $3)
			ON CONFLICT (message_id, user_id, emoji) DO NOTHING`, messageID, userID, emoji); err != nil {
			return nil, fmt.Errorf("add reaction: %w", err)
		}
	}
	msg, err := GetMessage(ctx, p, messageID)
	if errors.Is(err, errMessageNotFound) {
		return []ReactionSummary{}, nil
	}
	if err != nil {
		return nil, err
	}
	return msg.Reactions, nil
}
