package chat

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/db"
)

type DMChannel struct {
	ID          uuid.UUID `json:"id"`
	Recipient   auth.User `json:"recipient"`
	LastMessage *Message  `json:"last_message,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// GetOrCreateDMChannel finds or creates a 1-on-1 direct message channel between two users
func GetOrCreateDMChannel(ctx context.Context, p *db.Pool, user1ID, user2ID uuid.UUID) (*DMChannel, error) {
	if user1ID == user2ID {
		return nil, errors.New("cannot create a direct message channel with yourself")
	}

	u1, u2 := user1ID, user2ID
	if u1.String() > u2.String() {
		u1, u2 = u2, u1
	}

	var channelID uuid.UUID
	err := p.QueryRow(ctx, `
		SELECT channel_id FROM dm_channels WHERE user1_id = $1 AND user2_id = $2
	`, u1, u2).Scan(&channelID)

	if err == nil {
		return fetchDMChannel(ctx, p, channelID, user1ID, user2ID)
	}

	// Create new channel
	tx, err := p.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	err = tx.QueryRow(ctx, `
		INSERT INTO channels (name, type, topic)
		VALUES ('DM', 'dm', '')
		RETURNING id
	`).Scan(&channelID)
	if err != nil {
		return nil, fmt.Errorf("failed to insert channel: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO dm_channels (channel_id, user1_id, user2_id)
		VALUES ($1, $2, $3)
		ON CONFLICT (user1_id, user2_id) DO NOTHING
	`, channelID, u1, u2)
	if err != nil {
		return nil, fmt.Errorf("failed to insert dm_channels: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit tx: %w", err)
	}

	return fetchDMChannel(ctx, p, channelID, user1ID, user2ID)
}

func fetchDMChannel(ctx context.Context, p *db.Pool, channelID, currentUserID, targetUserID uuid.UUID) (*DMChannel, error) {
	recipientID := targetUserID
	if currentUserID == targetUserID {
		var u1, u2 uuid.UUID
		err := p.QueryRow(ctx, `SELECT user1_id, user2_id FROM dm_channels WHERE channel_id = $1`, channelID).Scan(&u1, &u2)
		if err != nil {
			return nil, err
		}
		if u1 == currentUserID {
			recipientID = u2
		} else {
			recipientID = u1
		}
	}

	var dm DMChannel
	dm.ID = channelID

	// Fetch recipient details
	var avatarKey *string
	err := p.QueryRow(ctx, `
		SELECT id, username, display_name, role, COALESCE(bio, ''), avatar_s3_key, created_at
		FROM users WHERE id = $1
	`, recipientID).Scan(&dm.Recipient.ID, &dm.Recipient.Username, &dm.Recipient.DisplayName, &dm.Recipient.Role, &dm.Recipient.Bio, &avatarKey, &dm.Recipient.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch recipient user: %w", err)
	}
	if avatarKey != nil && *avatarKey != "" {
		dm.Recipient.AvatarURL = fmt.Sprintf("/api/media/%s", *avatarKey)
	}

	// Fetch timestamps
	_ = p.QueryRow(ctx, `SELECT created_at, updated_at FROM dm_channels WHERE channel_id = $1`, channelID).Scan(&dm.CreatedAt, &dm.UpdatedAt)

	// Fetch optional last message
	var lastMsg Message
	err = p.QueryRow(ctx, `
		SELECT id, channel_id, user_id, content, is_edited, created_at
		FROM messages WHERE channel_id = $1 AND parent_id IS NULL
		ORDER BY created_at DESC LIMIT 1
	`, channelID).Scan(&lastMsg.ID, &lastMsg.ChannelID, &lastMsg.UserID, &lastMsg.Content, &lastMsg.IsEdited, &lastMsg.CreatedAt)
	if err == nil {
		dm.LastMessage = &lastMsg
	}

	return &dm, nil
}

// GetUserDMs returns all DM conversations for a given user
func GetUserDMs(ctx context.Context, p *db.Pool, userID uuid.UUID) ([]DMChannel, error) {
	rows, err := p.Query(ctx, `
		SELECT dm.channel_id,
		       CASE WHEN dm.user1_id = $1 THEN dm.user2_id ELSE dm.user1_id END AS recipient_id,
		       dm.created_at, dm.updated_at
		FROM dm_channels dm
		WHERE dm.user1_id = $1 OR dm.user2_id = $1
		ORDER BY dm.updated_at DESC
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to query dms: %w", err)
	}
	defer rows.Close()

	dms := make([]DMChannel, 0)
	for rows.Next() {
		var chID, recipID uuid.UUID
		var createdAt, updatedAt time.Time
		if err := rows.Scan(&chID, &recipID, &createdAt, &updatedAt); err != nil {
			return nil, err
		}

		dm, err := fetchDMChannel(ctx, p, chID, userID, recipID)
		if err != nil {
			continue
		}
		dms = append(dms, *dm)
	}

	return dms, nil
}

// CheckUserCanAccessChannel checks if the user has permission to read/write in this channel
func CheckUserCanAccessChannel(ctx context.Context, p *db.Pool, userID, channelID uuid.UUID) (bool, error) {
	var chType string
	err := p.QueryRow(ctx, `SELECT type FROM channels WHERE id = $1`, channelID).Scan(&chType)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}

	if chType != "dm" {
		return true, nil // Standard public text and voice channels are accessible to community members
	}

	var count int
	err = p.QueryRow(ctx, `
		SELECT COUNT(*) FROM dm_channels WHERE channel_id = $1 AND (user1_id = $2 OR user2_id = $2)
	`, channelID, userID).Scan(&count)
	if err != nil {
		return false, err
	}

	return count > 0, nil
}

// TouchDMChannel updates the updated_at timestamp of a DM channel when a message is sent
func TouchDMChannel(ctx context.Context, p *db.Pool, channelID uuid.UUID) {
	_, _ = p.Exec(ctx, `UPDATE dm_channels SET updated_at = NOW() WHERE channel_id = $1`, channelID)
}
