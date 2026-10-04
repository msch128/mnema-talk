package chat

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/db"
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
	Attachments []MediaAttachment `json:"attachments"`
	Reactions   []ReactionSummary `json:"reactions"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

// populateAttachments fetches media attachments for a slice of messages
func populateAttachments(ctx context.Context, p *db.Pool, messages []Message, msgMap map[uuid.UUID]int, msgIDs []uuid.UUID) {
	if len(msgIDs) == 0 {
		return
	}
	mediaRows, err := p.Query(ctx, `
		SELECT id, message_id, original_filename, mime_type, size_bytes, is_deleted
		FROM media
		WHERE message_id = ANY($1)
	`, msgIDs)
	if err == nil {
		defer mediaRows.Close()
		for mediaRows.Next() {
			var att MediaAttachment
			var msgID uuid.UUID
			if err := mediaRows.Scan(&att.ID, &msgID, &att.OriginalFilename, &att.MimeType, &att.SizeBytes, &att.IsDeleted); err == nil {
				att.URL = fmt.Sprintf("/api/media/%s", att.ID.String())
				if idx, ok := msgMap[msgID]; ok {
					messages[idx].Attachments = append(messages[idx].Attachments, att)
				}
			}
		}
	}
}

// populateReactions fetches grouped emoji reactions for a slice of messages
func populateReactions(ctx context.Context, p *db.Pool, messages []Message, msgMap map[uuid.UUID]int, msgIDs []uuid.UUID) {
	if len(msgIDs) == 0 {
		return
	}
	rows, err := p.Query(ctx, `
		SELECT message_id, emoji, COUNT(*), array_agg(user_id)
		FROM message_reactions
		WHERE message_id = ANY($1)
		GROUP BY message_id, emoji
		ORDER BY MIN(created_at) ASC
	`, msgIDs)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var msgID uuid.UUID
			var r ReactionSummary
			if err := rows.Scan(&msgID, &r.Emoji, &r.Count, &r.Users); err == nil {
				if idx, ok := msgMap[msgID]; ok {
					messages[idx].Reactions = append(messages[idx].Reactions, r)
				}
			}
		}
	}
}

func populateSingleReactions(ctx context.Context, p *db.Pool, m *Message) {
	rows, err := p.Query(ctx, `
		SELECT emoji, COUNT(*), array_agg(user_id)
		FROM message_reactions
		WHERE message_id = $1
		GROUP BY emoji
		ORDER BY MIN(created_at) ASC
	`, m.ID)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var r ReactionSummary
			if err := rows.Scan(&r.Emoji, &r.Count, &r.Users); err == nil {
				m.Reactions = append(m.Reactions, r)
			}
		}
	}
}

// GetChannelMessages fetches paginated root messages (parent_id IS NULL) with reply counts and attachments
func GetChannelMessages(ctx context.Context, p *db.Pool, channelID uuid.UUID, limit int, before *time.Time) ([]Message, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	var query string
	var args []interface{}

	if before != nil {
		query = `
			SELECT m.id, m.channel_id, m.user_id, m.parent_id, u.username, u.display_name, u.avatar_s3_key, m.content, m.is_pinned, COALESCE(m.is_edited, false),
			       (SELECT COUNT(*) FROM messages r WHERE r.parent_id = m.id) AS reply_count,
			       m.created_at, m.updated_at
			FROM messages m
			JOIN users u ON m.user_id = u.id
			WHERE m.channel_id = $1 AND m.parent_id IS NULL AND m.created_at < $2
			ORDER BY m.created_at DESC
			LIMIT $3
		`
		args = []interface{}{channelID, before, limit}
	} else {
		query = `
			SELECT m.id, m.channel_id, m.user_id, m.parent_id, u.username, u.display_name, u.avatar_s3_key, m.content, m.is_pinned, COALESCE(m.is_edited, false),
			       (SELECT COUNT(*) FROM messages r WHERE r.parent_id = m.id) AS reply_count,
			       m.created_at, m.updated_at
			FROM messages m
			JOIN users u ON m.user_id = u.id
			WHERE m.channel_id = $1 AND m.parent_id IS NULL
			ORDER BY m.created_at DESC
			LIMIT $2
		`
		args = []interface{}{channelID, limit}
	}

	rows, err := p.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query messages: %w", err)
	}
	defer rows.Close()

	messages := make([]Message, 0)
	msgIDs := make([]uuid.UUID, 0)
	msgMap := make(map[uuid.UUID]int)

	for rows.Next() {
		var m Message
		var avatarKey *string
		m.Attachments = make([]MediaAttachment, 0)
		m.Reactions = make([]ReactionSummary, 0)
		if err := rows.Scan(&m.ID, &m.ChannelID, &m.UserID, &m.ParentID, &m.Username, &m.DisplayName, &avatarKey, &m.Content, &m.IsPinned, &m.IsEdited, &m.ReplyCount, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, err
		}
		if avatarKey != nil && *avatarKey != "" {
			m.AvatarURL = fmt.Sprintf("/api/media/%s", *avatarKey)
		}
		msgMap[m.ID] = len(messages)
		msgIDs = append(msgIDs, m.ID)
		messages = append(messages, m)
	}

	populateAttachments(ctx, p, messages, msgMap, msgIDs)
	populateReactions(ctx, p, messages, msgMap, msgIDs)

	// Reverse slice so client receives chronological order (oldest to newest)
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}

	return messages, nil
}

// GetThreadReplies fetches all replies for a given root message in chronological order
func GetThreadReplies(ctx context.Context, p *db.Pool, parentID uuid.UUID) ([]Message, error) {
	query := `
		SELECT m.id, m.channel_id, m.user_id, m.parent_id, u.username, u.display_name, u.avatar_s3_key, m.content, m.is_pinned, COALESCE(m.is_edited, false),
		       0 AS reply_count, m.created_at, m.updated_at
		FROM messages m
		JOIN users u ON m.user_id = u.id
		WHERE m.parent_id = $1
		ORDER BY m.created_at ASC
	`
	rows, err := p.Query(ctx, query, parentID)
	if err != nil {
		return nil, fmt.Errorf("failed to query thread replies: %w", err)
	}
	defer rows.Close()

	replies := make([]Message, 0)
	msgIDs := make([]uuid.UUID, 0)
	msgMap := make(map[uuid.UUID]int)

	for rows.Next() {
		var m Message
		var avatarKey *string
		m.Attachments = make([]MediaAttachment, 0)
		m.Reactions = make([]ReactionSummary, 0)
		if err := rows.Scan(&m.ID, &m.ChannelID, &m.UserID, &m.ParentID, &m.Username, &m.DisplayName, &avatarKey, &m.Content, &m.IsPinned, &m.IsEdited, &m.ReplyCount, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, err
		}
		if avatarKey != nil && *avatarKey != "" {
			m.AvatarURL = fmt.Sprintf("/api/media/%s", *avatarKey)
		}
		msgMap[m.ID] = len(replies)
		msgIDs = append(msgIDs, m.ID)
		replies = append(replies, m)
	}

	populateAttachments(ctx, p, replies, msgMap, msgIDs)
	populateReactions(ctx, p, replies, msgMap, msgIDs)

	return replies, nil
}

// GetMessageByID retrieves a single message by its ID (including reply_count, attachments and reactions)
func GetMessageByID(ctx context.Context, p *db.Pool, messageID uuid.UUID) (*Message, error) {
	query := `
		SELECT m.id, m.channel_id, m.user_id, m.parent_id, u.username, u.display_name, u.avatar_s3_key, m.content, m.is_pinned, COALESCE(m.is_edited, false),
		       (SELECT COUNT(*) FROM messages r WHERE r.parent_id = m.id) AS reply_count,
		       m.created_at, m.updated_at
		FROM messages m
		JOIN users u ON m.user_id = u.id
		WHERE m.id = $1
	`
	var m Message
	var avatarKey *string
	m.Attachments = make([]MediaAttachment, 0)
	m.Reactions = make([]ReactionSummary, 0)
	err := p.QueryRow(ctx, query, messageID).Scan(
		&m.ID, &m.ChannelID, &m.UserID, &m.ParentID, &m.Username, &m.DisplayName, &avatarKey, &m.Content, &m.IsPinned, &m.IsEdited, &m.ReplyCount, &m.CreatedAt, &m.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if avatarKey != nil && *avatarKey != "" {
		m.AvatarURL = fmt.Sprintf("/api/media/%s", *avatarKey)
	}

	// Fetch attachments for this message
	mediaRows, err := p.Query(ctx, `
		SELECT id, message_id, original_filename, mime_type, size_bytes, is_deleted
		FROM media
		WHERE message_id = $1
	`, m.ID)
	if err == nil {
		defer mediaRows.Close()
		for mediaRows.Next() {
			var att MediaAttachment
			var msgID uuid.UUID
			if err := mediaRows.Scan(&att.ID, &msgID, &att.OriginalFilename, &att.MimeType, &att.SizeBytes, &att.IsDeleted); err == nil {
				att.URL = fmt.Sprintf("/api/media/%s", att.ID.String())
				m.Attachments = append(m.Attachments, att)
			}
		}
	}

	populateSingleReactions(ctx, p, &m)

	return &m, nil
}

// CreateMessage inserts a new chat message (or thread reply if parentID != nil) into PostgreSQL
func CreateMessage(ctx context.Context, p *db.Pool, channelID, userID uuid.UUID, content string, parentID *uuid.UUID) (*Message, error) {
	var m Message
	var avatarKey *string
	m.Attachments = make([]MediaAttachment, 0)
	m.Reactions = make([]ReactionSummary, 0)

	query := `
		WITH inserted AS (
			INSERT INTO messages (channel_id, user_id, content, parent_id)
			VALUES ($1, $2, $3, $4)
			RETURNING id, channel_id, user_id, parent_id, content, is_pinned, is_edited, created_at, updated_at
		)
		SELECT i.id, i.channel_id, i.user_id, i.parent_id, u.username, u.display_name, u.avatar_s3_key, i.content, i.is_pinned, i.is_edited, 0, i.created_at, i.updated_at
		FROM inserted i
		JOIN users u ON i.user_id = u.id
	`

	err := p.QueryRow(ctx, query, channelID, userID, content, parentID).Scan(
		&m.ID, &m.ChannelID, &m.UserID, &m.ParentID, &m.Username, &m.DisplayName, &avatarKey, &m.Content, &m.IsPinned, &m.IsEdited, &m.ReplyCount, &m.CreatedAt, &m.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to insert message: %w", err)
	}
	if avatarKey != nil && *avatarKey != "" {
		m.AvatarURL = fmt.Sprintf("/api/media/%s", *avatarKey)
	}

	return &m, nil
}

// EditMessage updates the message text and sets is_edited = true if the user is the author
func EditMessage(ctx context.Context, p *db.Pool, messageID, userID uuid.UUID, content string) (*Message, error) {
	tag, err := p.Exec(ctx, `
		UPDATE messages
		SET content = $1, is_edited = TRUE, updated_at = NOW()
		WHERE id = $2 AND user_id = $3
	`, content, messageID, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to update message: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, fmt.Errorf("message not found or not authorized to edit")
	}

	return GetMessageByID(ctx, p, messageID)
}

// DeleteMessage deletes the message if the user is the author or an admin
func DeleteMessage(ctx context.Context, p *db.Pool, messageID, userID uuid.UUID, isAdmin bool) error {
	var err error
	var rowsAffected int64
	if isAdmin {
		tag, e := p.Exec(ctx, `DELETE FROM messages WHERE id = $1`, messageID)
		err = e
		rowsAffected = tag.RowsAffected()
	} else {
		tag, e := p.Exec(ctx, `DELETE FROM messages WHERE id = $1 AND user_id = $2`, messageID, userID)
		err = e
		rowsAffected = tag.RowsAffected()
	}
	if err != nil {
		return fmt.Errorf("failed to delete message: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("message not found or not authorized to delete")
	}
	return nil
}

// ToggleReaction toggles a user's emoji reaction on a message and returns the updated reaction list
func ToggleReaction(ctx context.Context, p *db.Pool, messageID, userID uuid.UUID, emoji string) ([]ReactionSummary, error) {
	tag, err := p.Exec(ctx, `
		DELETE FROM message_reactions WHERE message_id = $1 AND user_id = $2 AND emoji = $3
	`, messageID, userID, emoji)
	if err != nil {
		return nil, fmt.Errorf("failed to toggle reaction: %w", err)
	}

	if tag.RowsAffected() == 0 {
		_, err := p.Exec(ctx, `
			INSERT INTO message_reactions (message_id, user_id, emoji)
			VALUES ($1, $2, $3)
			ON CONFLICT (message_id, user_id, emoji) DO NOTHING
		`, messageID, userID, emoji)
		if err != nil {
			return nil, fmt.Errorf("failed to add reaction: %w", err)
		}
	}

	rows, err := p.Query(ctx, `
		SELECT emoji, COUNT(*), array_agg(user_id)
		FROM message_reactions
		WHERE message_id = $1
		GROUP BY emoji
		ORDER BY MIN(created_at) ASC
	`, messageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	reactions := make([]ReactionSummary, 0)
	for rows.Next() {
		var r ReactionSummary
		if err := rows.Scan(&r.Emoji, &r.Count, &r.Users); err == nil {
			reactions = append(reactions, r)
		}
	}

	return reactions, nil
}
