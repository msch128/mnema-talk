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
	ReplyCount  int               `json:"reply_count"`
	Attachments []MediaAttachment `json:"attachments"`
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

// GetChannelMessages fetches paginated root messages (parent_id IS NULL) with reply counts and attachments
func GetChannelMessages(ctx context.Context, p *db.Pool, channelID uuid.UUID, limit int, before *time.Time) ([]Message, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	var query string
	var args []interface{}

	if before != nil {
		query = `
			SELECT m.id, m.channel_id, m.user_id, m.parent_id, u.username, u.display_name, u.avatar_s3_key, m.content, m.is_pinned,
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
			SELECT m.id, m.channel_id, m.user_id, m.parent_id, u.username, u.display_name, u.avatar_s3_key, m.content, m.is_pinned,
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
		if err := rows.Scan(&m.ID, &m.ChannelID, &m.UserID, &m.ParentID, &m.Username, &m.DisplayName, &avatarKey, &m.Content, &m.IsPinned, &m.ReplyCount, &m.CreatedAt, &m.UpdatedAt); err != nil {
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

	// Reverse slice so client receives chronological order (oldest to newest)
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}

	return messages, nil
}

// GetThreadReplies fetches all replies for a given root message in chronological order
func GetThreadReplies(ctx context.Context, p *db.Pool, parentID uuid.UUID) ([]Message, error) {
	query := `
		SELECT m.id, m.channel_id, m.user_id, m.parent_id, u.username, u.display_name, u.avatar_s3_key, m.content, m.is_pinned,
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
		if err := rows.Scan(&m.ID, &m.ChannelID, &m.UserID, &m.ParentID, &m.Username, &m.DisplayName, &avatarKey, &m.Content, &m.IsPinned, &m.ReplyCount, &m.CreatedAt, &m.UpdatedAt); err != nil {
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

	return replies, nil
}

// GetMessageByID retrieves a single message by its ID (including reply_count and attachments)
func GetMessageByID(ctx context.Context, p *db.Pool, messageID uuid.UUID) (*Message, error) {
	query := `
		SELECT m.id, m.channel_id, m.user_id, m.parent_id, u.username, u.display_name, u.avatar_s3_key, m.content, m.is_pinned,
		       (SELECT COUNT(*) FROM messages r WHERE r.parent_id = m.id) AS reply_count,
		       m.created_at, m.updated_at
		FROM messages m
		JOIN users u ON m.user_id = u.id
		WHERE m.id = $1
	`
	var m Message
	var avatarKey *string
	m.Attachments = make([]MediaAttachment, 0)
	err := p.QueryRow(ctx, query, messageID).Scan(
		&m.ID, &m.ChannelID, &m.UserID, &m.ParentID, &m.Username, &m.DisplayName, &avatarKey, &m.Content, &m.IsPinned, &m.ReplyCount, &m.CreatedAt, &m.UpdatedAt,
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

	return &m, nil
}

// CreateMessage inserts a new chat message (or thread reply if parentID != nil) into PostgreSQL
func CreateMessage(ctx context.Context, p *db.Pool, channelID, userID uuid.UUID, content string, parentID *uuid.UUID) (*Message, error) {
	var m Message
	var avatarKey *string
	m.Attachments = make([]MediaAttachment, 0)

	query := `
		WITH inserted AS (
			INSERT INTO messages (channel_id, user_id, content, parent_id)
			VALUES ($1, $2, $3, $4)
			RETURNING id, channel_id, user_id, parent_id, content, is_pinned, created_at, updated_at
		)
		SELECT i.id, i.channel_id, i.user_id, i.parent_id, u.username, u.display_name, u.avatar_s3_key, i.content, i.is_pinned, 0, i.created_at, i.updated_at
		FROM inserted i
		JOIN users u ON i.user_id = u.id
	`

	err := p.QueryRow(ctx, query, channelID, userID, content, parentID).Scan(
		&m.ID, &m.ChannelID, &m.UserID, &m.ParentID, &m.Username, &m.DisplayName, &avatarKey, &m.Content, &m.IsPinned, &m.ReplyCount, &m.CreatedAt, &m.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to insert message: %w", err)
	}
	if avatarKey != nil && *avatarKey != "" {
		m.AvatarURL = fmt.Sprintf("/api/media/%s", *avatarKey)
	}

	return &m, nil
}
