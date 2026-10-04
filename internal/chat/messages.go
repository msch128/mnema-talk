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
	Username    string            `json:"username"`
	DisplayName string            `json:"display_name"`
	AvatarURL   string            `json:"avatar_url,omitempty"`
	Content     string            `json:"content"`
	IsPinned    bool              `json:"is_pinned"`
	Attachments []MediaAttachment `json:"attachments"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

// GetChannelMessages fetches paginated messages with author details and media attachments
func GetChannelMessages(ctx context.Context, p *db.Pool, channelID uuid.UUID, limit int, before *time.Time) ([]Message, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	var query string
	var args []interface{}

	if before != nil {
		query = `
			SELECT m.id, m.channel_id, m.user_id, u.username, u.display_name, m.content, m.is_pinned, m.created_at, m.updated_at
			FROM messages m
			JOIN users u ON m.user_id = u.id
			WHERE m.channel_id = $1 AND m.created_at < $2
			ORDER BY m.created_at DESC
			LIMIT $3
		`
		args = []interface{}{channelID, before, limit}
	} else {
		query = `
			SELECT m.id, m.channel_id, m.user_id, u.username, u.display_name, m.content, m.is_pinned, m.created_at, m.updated_at
			FROM messages m
			JOIN users u ON m.user_id = u.id
			WHERE m.channel_id = $1
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

	var messages []Message
	msgIDs := make([]uuid.UUID, 0)
	msgMap := make(map[uuid.UUID]int)

	for rows.Next() {
		var m Message
		m.Attachments = make([]MediaAttachment, 0)
		if err := rows.Scan(&m.ID, &m.ChannelID, &m.UserID, &m.Username, &m.DisplayName, &m.Content, &m.IsPinned, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, err
		}
		msgMap[m.ID] = len(messages)
		msgIDs = append(msgIDs, m.ID)
		messages = append(messages, m)
	}

	// Fetch media attachments for these messages
	if len(msgIDs) > 0 {
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

	// Reverse slice so client receives chronological order (oldest to newest)
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}

	return messages, nil
}

// CreateMessage inserts a new chat message into PostgreSQL
func CreateMessage(ctx context.Context, p *db.Pool, channelID, userID uuid.UUID, content string) (*Message, error) {
	var m Message
	m.Attachments = make([]MediaAttachment, 0)

	query := `
		WITH inserted AS (
			INSERT INTO messages (channel_id, user_id, content)
			VALUES ($1, $2, $3)
			RETURNING id, channel_id, user_id, content, is_pinned, created_at, updated_at
		)
		SELECT i.id, i.channel_id, i.user_id, u.username, u.display_name, i.content, i.is_pinned, i.created_at, i.updated_at
		FROM inserted i
		JOIN users u ON i.user_id = u.id
	`

	err := p.QueryRow(ctx, query, channelID, userID, content).Scan(
		&m.ID, &m.ChannelID, &m.UserID, &m.Username, &m.DisplayName, &m.Content, &m.IsPinned, &m.CreatedAt, &m.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to insert message: %w", err)
	}

	return &m, nil
}
