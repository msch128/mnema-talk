package messages

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/chat/mentions"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// MessageSelect loads messages with author, reply count and the quoted reply
// preview in one round trip, of messages whose lifetime has not ended
// (core.LiveMessages), even before the sweeper removed an ended one. rm/ru are the replied-to message and its author;
// the quoted snippet sent with every reply is mdplain.Snippet of the whole
// replied-to message (at most core.MaxMessageLen characters are read).
const MessageSelect = `
	SELECT m.id, m.number, m.channel_id, m.user_id, m.parent_id, u.username, COALESCE(m.author_name, u.display_name), u.avatar_s3_key,
	       m.content, m.is_pinned, m.is_edited,
	       (SELECT COUNT(*) FROM messages r WHERE r.parent_id = m.id AND (r.expires_at IS NULL OR r.expires_at > now())),
	       m.created_at, m.updated_at,
	       m.reply_to_id, rm.id, rm.user_id, ru.username, ru.display_name, ru.avatar_s3_key,
	       rm.content,
	       CASE WHEN rm.id IS NULL THEN FALSE
	            ELSE EXISTS (SELECT 1 FROM media md WHERE md.message_id = rm.id) END,
	       u.is_bot, (u.is_bot AND u.username LIKE 'wh-%'), m.embeds, ru.is_bot, (ru.is_bot AND ru.username LIKE 'wh-%'),
	       m.pinned_at, pu.id, pu.username, pu.display_name, pu.avatar_s3_key,
	       m.forwarded, m.expires_at
	FROM `+core.LiveMessages+` m
	JOIN users u ON u.id = m.user_id
	LEFT JOIN `+core.LiveMessages+` rm ON rm.id = m.reply_to_id
	LEFT JOIN users ru ON ru.id = rm.user_id
	LEFT JOIN users pu ON pu.id = m.pinned_by AND m.is_pinned`

func ScanMessages(rows pgx.Rows) ([]core.Message, error) {
	defer rows.Close()
	msgs := make([]core.Message, 0)
	for rows.Next() {
		m := core.Message{Attachments: make([]core.MediaAttachment, 0), Reactions: make([]core.ReactionSummary, 0), Mentions: make([]uuid.UUID, 0), DirectMentions: make([]uuid.UUID, 0), MentionRoles: make([]uuid.UUID, 0)}
		var avatar *string
		var r rawReplyFields
		var embeds []byte
		var pin rawPinner
		var fwd rawForward
		if err := rows.Scan(&m.ID, &m.Number, &m.ChannelID, &m.UserID, &m.ParentID, &m.Username, &m.DisplayName, &avatar,
			&m.Content, &m.IsPinned, &m.IsEdited, &m.ReplyCount, &m.CreatedAt, &m.UpdatedAt,
			&m.ReplyToID, &r.id, &r.user, &r.username, &r.displayName, &r.avatar, &r.content, &r.hasMedia, &m.IsBot, &m.IsWebhook, &embeds, &r.isBot, &r.isWebhook,
			&m.PinnedAt, &pin.id, &pin.username, &pin.displayName, &pin.avatar,
			&fwd.forwarded, &m.ExpiresAt); err != nil {
			return nil, err
		}
		var err error
		if m.Embeds, err = DecodeEmbeds(embeds); err != nil {
			return nil, err
		}
		m.AvatarURL = auth.AvatarURL(avatar)
		m.PinnedBy = pin.ref()
		m.Forward = fwd.ref()
		if m.ReplyToID != nil {
			m.ReplyTo = buildReplyPreview(*m.ReplyToID, r)
		}
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

// Enrich loads attachments, reactions, mentions and polls for msgs.
func Enrich(ctx context.Context, p db.Querier, msgs []core.Message) error {
	if len(msgs) == 0 {
		return nil
	}
	idx := make(map[uuid.UUID]int, len(msgs))
	ids := make([]uuid.UUID, len(msgs))
	for i, m := range msgs {
		idx[m.ID] = i
		ids[i] = m.ID
	}
	for _, load := range []func(context.Context, db.Querier, []core.Message, map[uuid.UUID]int, []uuid.UUID) error{
		loadAttachments, loadReactions, loadMentions, mentions.LoadMentionRoles, loadPolls, loadForumPostDetails,
	} {
		if err := load(ctx, p, msgs, idx, ids); err != nil {
			return err
		}
	}
	return nil
}

// loadAttachments fills msgs[].Attachments; idx maps a message ID to its
// position in msgs.
func loadAttachments(ctx context.Context, p db.Querier, msgs []core.Message, idx map[uuid.UUID]int, ids []uuid.UUID) error {
	rows, err := p.Query(ctx, `
		SELECT id, message_id, original_filename, mime_type, size_bytes, is_deleted, width, height
		FROM media WHERE message_id = ANY($1) ORDER BY position, created_at`, ids)
	if err != nil {
		return fmt.Errorf("load attachments: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var a core.MediaAttachment
		var msgID uuid.UUID
		if err := rows.Scan(&a.ID, &msgID, &a.OriginalFilename, &a.MimeType, &a.SizeBytes, &a.IsDeleted, &a.Width, &a.Height); err != nil {
			return err
		}
		a.URL = "/api/media/" + a.ID.String()
		msgs[idx[msgID]].Attachments = append(msgs[idx[msgID]].Attachments, a)
	}
	return rows.Err()
}

func loadReactions(ctx context.Context, p db.Querier, msgs []core.Message, idx map[uuid.UUID]int, ids []uuid.UUID) error {
	rows, err := p.Query(ctx, `
		SELECT message_id, MIN(emoji), array_agg(user_id ORDER BY created_at)
		FROM message_reactions WHERE message_id = ANY($1)
		GROUP BY message_id, `+reactionKeySQL+` ORDER BY MIN(created_at)`, ids)
	if err != nil {
		return fmt.Errorf("load reactions: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var r core.ReactionSummary
		var msgID uuid.UUID
		if err := rows.Scan(&msgID, &r.Emoji, &r.Users); err != nil {
			return err
		}
		msgs[idx[msgID]].Reactions = append(msgs[idx[msgID]].Reactions, oneReaction(r))
	}
	return rows.Err()
}

func loadMentions(ctx context.Context, p db.Querier, msgs []core.Message, idx map[uuid.UUID]int, ids []uuid.UUID) error {
	rows, err := p.Query(ctx, `SELECT message_id, user_id, kind <> $2 FROM message_mentions WHERE message_id = ANY($1)`, ids, core.MentionKindGroup)
	if err != nil {
		return fmt.Errorf("load mentions: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var msgID, userID uuid.UUID
		var direct bool
		if err := rows.Scan(&msgID, &userID, &direct); err != nil {
			return err
		}
		m := &msgs[idx[msgID]]
		m.Mentions = append(m.Mentions, userID)
		if direct {
			m.DirectMentions = append(m.DirectMentions, userID)
		}
	}
	return rows.Err()
}

func QueryMessages(ctx context.Context, p db.Querier, sql string, args ...any) ([]core.Message, error) {
	rows, err := p.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("query messages: %w", err)
	}
	msgs, err := ScanMessages(rows)
	if err != nil {
		return nil, err
	}
	return msgs, Enrich(ctx, p, msgs)
}

// rootCursor is the (created_at, id) position of an anchor message; the id
// breaks ties between messages created in the same microsecond.
const rootCursor = `(SELECT created_at, id FROM messages WHERE id = $2)`

// GetChannelMessages returns a page of root messages, always oldest first.
func GetChannelMessages(ctx context.Context, p *db.Pool, channelID uuid.UUID, q core.HistoryQuery) ([]core.Message, error) {
	switch {
	case q.Before != nil:
		return olderThan(ctx, p, channelID, *q.Before, q.Limit, false)
	case q.After != nil:
		return NewerThan(ctx, p, channelID, *q.After, q.Limit)
	case q.Around != nil:
		// Half the page before the anchor (including it), the rest after.
		older, err := olderThan(ctx, p, channelID, *q.Around, q.Limit/2+1, true)
		if err != nil {
			return nil, err
		}
		newer, err := NewerThan(ctx, p, channelID, *q.Around, q.Limit-len(older))
		if err != nil {
			return nil, err
		}
		return append(older, newer...), nil
	}
	msgs, err := QueryMessages(ctx, p, MessageSelect+`
		WHERE m.channel_id = $1 AND m.parent_id IS NULL
		ORDER BY m.created_at DESC, m.id DESC
		LIMIT $2`, channelID, q.Limit)
	if err != nil {
		return nil, err
	}
	reverse(msgs)
	return msgs, nil
}

func olderThan(ctx context.Context, p *db.Pool, channelID, anchor uuid.UUID, limit int, inclusive bool) ([]core.Message, error) {
	op := "<"
	if inclusive {
		op = "<="
	}
	msgs, err := QueryMessages(ctx, p, MessageSelect+`
		WHERE m.channel_id = $1 AND m.parent_id IS NULL AND (m.created_at, m.id) `+op+` `+rootCursor+`
		ORDER BY m.created_at DESC, m.id DESC
		LIMIT $3`, channelID, anchor, limit)
	if err != nil {
		return nil, err
	}
	reverse(msgs)
	return msgs, nil
}

func NewerThan(ctx context.Context, p *db.Pool, channelID, anchor uuid.UUID, limit int) ([]core.Message, error) {
	if limit <= 0 {
		return []core.Message{}, nil
	}
	return QueryMessages(ctx, p, MessageSelect+`
		WHERE m.channel_id = $1 AND m.parent_id IS NULL AND (m.created_at, m.id) > `+rootCursor+`
		ORDER BY m.created_at ASC, m.id ASC
		LIMIT $3`, channelID, anchor, limit)
}

func reverse(msgs []core.Message) {
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
}

func IsSameThreadContext(refParentID, parentID, replyTo *uuid.UUID) bool {
	if refParentID == nil && parentID == nil {
		return true // both root messages
	}
	if refParentID != nil && parentID != nil {
		return *refParentID == *parentID // both replies in the same thread
	}
	if refParentID == nil && parentID != nil {
		return *replyTo == *parentID // reply to thread root
	}
	return false
}

// ValidateReplyTargetFor checks that replyTo exists in the same channel and the
// same thread context (both root messages, or both replies of one thread). A
// caller who sees what view lets them see gets a message in a channel they
// cannot see answered as a missing message; a nil view asks nobody.
func ValidateReplyTargetFor(ctx context.Context, p *db.Pool, view core.Viewer, channelID uuid.UUID, parentID, replyTo *uuid.UUID) error {
	if replyTo == nil {
		return nil
	}
	ref, err := core.LoadReferencedMessage(ctx, p, view, channelID, *replyTo, "message to reply to not found")
	if err != nil {
		return err
	}
	if ref.ChannelID != channelID || !IsSameThreadContext(ref.ParentID, parentID, replyTo) {
		return httpx.ErrInvalidInput("can only reply to a message in the same conversation")
	}
	return nil
}

// GetThreadReplies returns a page of replies of a root message, oldest first.
func GetThreadReplies(ctx context.Context, p *db.Pool, parentID uuid.UUID, q core.HistoryQuery) ([]core.Message, error) {
	if q.Limit <= 0 {
		q.Limit = 50
	}
	if q.Before != nil {
		msgs, err := QueryMessages(ctx, p, MessageSelect+`
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
		return QueryMessages(ctx, p, MessageSelect+`
			WHERE m.parent_id = $1 AND (m.created_at, m.id) > `+rootCursor+`
			ORDER BY m.created_at ASC, m.id ASC
			LIMIT $3`, parentID, *q.After, q.Limit)
	}
	return QueryMessages(ctx, p, MessageSelect+`
		WHERE m.parent_id = $1
		ORDER BY m.created_at ASC, m.id ASC
		LIMIT $2`, parentID, q.Limit)
}

// ServeGetMessageByNumber returns one fully populated message by its link number.
func GetMessageByNumber(ctx context.Context, p *db.Pool, number int64) (*core.Message, error) {
	msgs, err := QueryMessages(ctx, p, MessageSelect+` WHERE m.number = $1`, number)
	if err != nil {
		return nil, err
	}
	if len(msgs) == 0 {
		return nil, core.ErrMessageNotFound
	}
	return &msgs[0], nil
}

// GetMessage returns one fully populated message.
func GetMessage(ctx context.Context, p db.Querier, messageID uuid.UUID) (*core.Message, error) {
	msgs, err := QueryMessages(ctx, p, MessageSelect+` WHERE m.id = $1`, messageID)
	if err != nil {
		return nil, err
	}
	if len(msgs) == 0 {
		return nil, core.ErrMessageNotFound
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
	// ForwardOf is the root message this one forwards (never a copy of it).
	ForwardOf *uuid.UUID
	// WebhookID, AuthorName and Embeds are set for webhook messages only.
	WebhookID  *uuid.UUID
	AuthorName string
	Embeds     []core.Embed
	// HasAttachment is set for an upload (automod's new-account rule looks at it).
	HasAttachment bool
	// Title is set for root messages in a forum channel.
	Title *string
	// IsLocked is set when creating a locked post.
	IsLocked bool
	// ScreenText is more member-written text of the message (poll options)
	// that automod reads together with Title and Content.
	ScreenText []string
	// Record marks a message the server writes on a member's behalf (the
	// "missed call" note): it does not count towards the cap on a starter's
	// unanswered messages in a conversation.
	Record bool
	// Nonce is the client's id for this send; a second send with it returns
	// the first message (message_nonces, migration 0100).
	Nonce *uuid.UUID
	// LifetimeSeconds, when above 0, is the sender's lifetime (gateLifetime).
	LifetimeSeconds int
}

// ServeCreateMessage inserts a message and returns its ID.
func CreateMessage(ctx context.Context, q db.Querier, nm NewMessage) (uuid.UUID, error) {
	var id uuid.UUID
	embeds := []byte("[]")
	if len(nm.Embeds) > 0 {
		var err error
		embeds, err = json.Marshal(nm.Embeds)
		if err != nil {
			return uuid.Nil, fmt.Errorf("marshal embeds: %w", err)
		}
	}
	var authorName *string
	if nm.AuthorName != "" {
		authorName = &nm.AuthorName
	}
	// created_at is taken now, not at the start of the transaction (NOW()):
	// after waiting for the locks of gatePost, a message must not sort before
	// a read marker or a neighbour that committed meanwhile.
	// A message of a direct message conversation draws its number from the
	// conversations' own sequence, so the public numbering shows nothing of
	// what is said in private (migration 0050).
	err := q.QueryRow(ctx, `
		INSERT INTO messages (number, in_dm, channel_id, user_id, content, parent_id, reply_to_id, webhook_id, author_name, embeds, forward_of, forwarded, title, is_locked, created_at, expires_at) OVERRIDING SYSTEM VALUE
		SELECT CASE WHEN c.type = 'dm' THEN nextval('dm_number_seq') ELSE nextval(pg_get_serial_sequence('messages', 'number')) END,
		       c.type = 'dm', c.id, $2, $3, $4, $5, $6, $7, $8, $9::uuid, $9::uuid IS NOT NULL, $10, $11, t.at,
		       CASE WHEN $12::int > 0 THEN t.at + make_interval(secs => $12::int) END
		FROM channels c, (SELECT clock_timestamp() AS at) t WHERE c.id = $1
		RETURNING id`, nm.ChannelID, nm.UserID, nm.Content, nm.ParentID, nm.ReplyToID, nm.WebhookID, authorName, embeds, nm.ForwardOf, nm.Title, nm.IsLocked, nm.LifetimeSeconds).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, core.ErrChannelNotFound
	}
	if IsForeignKeyViolation(err) && nm.ParentID != nil {
		// The thread root was deleted after ValidateParent looked at it.
		return uuid.Nil, httpx.ErrInvalidInput("parent message not found")
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("insert message: %w", err)
	}
	return id, nil
}

// IsForeignKeyViolation reports whether err is the database refusing a row
// because the row it refers to is gone (SQLSTATE 23503): a concurrent delete
// won against a reference that was validated moments earlier.
func IsForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503"
}
