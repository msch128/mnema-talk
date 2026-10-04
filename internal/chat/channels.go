package chat

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/db"
)

type ChannelType string

const (
	ChannelTypeText  ChannelType = "text"
	ChannelTypeVoice ChannelType = "voice"
)

type Channel struct {
	ID         uuid.UUID   `json:"id"`
	CategoryID *uuid.UUID  `json:"category_id"`
	Name       string      `json:"name"`
	Type       ChannelType `json:"type"`
	Topic      string      `json:"topic"`
	SortOrder  int         `json:"sort_order"`
	CreatedAt  time.Time   `json:"created_at"`
}

type Category struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	SortOrder int       `json:"sort_order"`
	Channels  []Channel `json:"channels"`
	CreatedAt time.Time `json:"created_at"`
}

// GetServerHierarchy returns all categories with their nested channels, plus uncategorized channels
func GetServerHierarchy(ctx context.Context, p *db.Pool) ([]Category, []Channel, error) {
	// 1. Fetch all categories
	catRows, err := p.Query(ctx, `SELECT id, name, sort_order, created_at FROM categories ORDER BY sort_order ASC, created_at ASC`)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to query categories: %w", err)
	}
	defer catRows.Close()

	var categories []Category
	categoryMap := make(map[uuid.UUID]int)

	for catRows.Next() {
		var cat Category
		cat.Channels = make([]Channel, 0)
		if err := catRows.Scan(&cat.ID, &cat.Name, &cat.SortOrder, &cat.CreatedAt); err != nil {
			return nil, nil, err
		}
		categoryMap[cat.ID] = len(categories)
		categories = append(categories, cat)
	}

	// 2. Fetch all channels
	chanRows, err := p.Query(ctx, `SELECT id, category_id, name, type, topic, sort_order, created_at FROM channels ORDER BY sort_order ASC, created_at ASC`)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to query channels: %w", err)
	}
	defer chanRows.Close()

	var uncategorized []Channel

	for chanRows.Next() {
		var ch Channel
		if err := chanRows.Scan(&ch.ID, &ch.CategoryID, &ch.Name, &ch.Type, &ch.Topic, &ch.SortOrder, &ch.CreatedAt); err != nil {
			return nil, nil, err
		}

		if ch.CategoryID != nil {
			if idx, ok := categoryMap[*ch.CategoryID]; ok {
				categories[idx].Channels = append(categories[idx].Channels, ch)
				continue
			}
		}
		uncategorized = append(uncategorized, ch)
	}

	return categories, uncategorized, nil
}

// CreateCategory adds a new channel category (Admin only)
func CreateCategory(ctx context.Context, p *db.Pool, name string, sortOrder int) (*Category, error) {
	var cat Category
	cat.Channels = make([]Channel, 0)
	query := `INSERT INTO categories (name, sort_order) VALUES ($1, $2) RETURNING id, name, sort_order, created_at`
	err := p.QueryRow(ctx, query, name, sortOrder).Scan(&cat.ID, &cat.Name, &cat.SortOrder, &cat.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to create category: %w", err)
	}
	return &cat, nil
}

// CreateChannel adds a new text or voice channel (Admin only)
func CreateChannel(ctx context.Context, p *db.Pool, categoryID *uuid.UUID, name string, chType ChannelType, topic string, sortOrder int) (*Channel, error) {
	var ch Channel
	query := `INSERT INTO channels (category_id, name, type, topic, sort_order) VALUES ($1, $2, $3, $4, $5) RETURNING id, category_id, name, type, topic, sort_order, created_at`
	err := p.QueryRow(ctx, query, categoryID, name, chType, topic, sortOrder).Scan(&ch.ID, &ch.CategoryID, &ch.Name, &ch.Type, &ch.Topic, &ch.SortOrder, &ch.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to create channel: %w", err)
	}
	return &ch, nil
}

// DeleteChannel removes a channel (Admin only)
func DeleteChannel(ctx context.Context, p *db.Pool, channelID uuid.UUID) error {
	_, err := p.Exec(ctx, `DELETE FROM channels WHERE id = $1`, channelID)
	return err
}

// DeleteCategory removes a category and unlinks its channels (Admin only)
func DeleteCategory(ctx context.Context, p *db.Pool, categoryID uuid.UUID) error {
	_, err := p.Exec(ctx, `DELETE FROM categories WHERE id = $1`, categoryID)
	return err
}
