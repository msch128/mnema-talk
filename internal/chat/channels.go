package chat

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

type ChannelType string

const (
	ChannelTypeText  ChannelType = "text"
	ChannelTypeVoice ChannelType = "voice"

	MaxChannelNameLen = 64
	MaxTopicLen       = 255
)

type Channel struct {
	ID uuid.UUID `json:"id" format:"uuid"`
	// Number is the channel's short, stable link number (/c/12-general).
	Number     int64       `json:"number"`
	CategoryID *uuid.UUID  `json:"category_id" format:"uuid" extensions:"x-nullable"`
	Name       string      `json:"name" maxLength:"64"`
	Type       ChannelType `json:"type"`
	Topic      string      `json:"topic" maxLength:"255"`
	SortOrder  int         `json:"sort_order"`
	CreatedAt  time.Time   `json:"created_at" format:"date-time"`
}

type Category struct {
	ID        uuid.UUID `json:"id" format:"uuid"`
	Name      string    `json:"name" maxLength:"64"`
	SortOrder int       `json:"sort_order"`
	Channels  []Channel `json:"channels"`
	CreatedAt time.Time `json:"created_at" format:"date-time"`
}

// GetServerHierarchy returns all categories with their nested channels, plus
// uncategorized channels.
func GetServerHierarchy(ctx context.Context, p *db.Pool) ([]Category, []Channel, error) {
	catRows, err := p.Query(ctx, `SELECT id, name, sort_order, created_at FROM categories ORDER BY sort_order, created_at`)
	if err != nil {
		return nil, nil, fmt.Errorf("query categories: %w", err)
	}
	categories := make([]Category, 0)
	categoryIdx := make(map[uuid.UUID]int)
	for catRows.Next() {
		cat := Category{Channels: make([]Channel, 0)}
		if err := catRows.Scan(&cat.ID, &cat.Name, &cat.SortOrder, &cat.CreatedAt); err != nil {
			catRows.Close()
			return nil, nil, err
		}
		categoryIdx[cat.ID] = len(categories)
		categories = append(categories, cat)
	}
	catRows.Close()
	if err := catRows.Err(); err != nil {
		return nil, nil, err
	}

	chanRows, err := p.Query(ctx, `
		SELECT id, number, category_id, name, type, topic, sort_order, created_at
		FROM channels ORDER BY sort_order, created_at`)
	if err != nil {
		return nil, nil, fmt.Errorf("query channels: %w", err)
	}
	defer chanRows.Close()

	uncategorized := make([]Channel, 0)
	for chanRows.Next() {
		var ch Channel
		if err := chanRows.Scan(&ch.ID, &ch.Number, &ch.CategoryID, &ch.Name, &ch.Type, &ch.Topic, &ch.SortOrder, &ch.CreatedAt); err != nil {
			return nil, nil, err
		}
		if ch.CategoryID != nil {
			if idx, ok := categoryIdx[*ch.CategoryID]; ok {
				categories[idx].Channels = append(categories[idx].Channels, ch)
				continue
			}
		}
		uncategorized = append(uncategorized, ch)
	}
	return categories, uncategorized, chanRows.Err()
}

func CreateCategory(ctx context.Context, p *db.Pool, name string, sortOrder int) (*Category, error) {
	name, err := httpx.CleanText("name", name, MaxChannelNameLen, true)
	if err != nil {
		return nil, err
	}
	cat := Category{Channels: make([]Channel, 0)}
	err = p.QueryRow(ctx, `
		INSERT INTO categories (name, sort_order) VALUES ($1, $2)
		RETURNING id, name, sort_order, created_at`, name, sortOrder).
		Scan(&cat.ID, &cat.Name, &cat.SortOrder, &cat.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("create category: %w", err)
	}
	return &cat, nil
}

func CreateChannel(ctx context.Context, p *db.Pool, categoryID *uuid.UUID, name string, chType ChannelType, topic string, sortOrder int) (*Channel, error) {
	if chType != ChannelTypeText && chType != ChannelTypeVoice {
		return nil, httpx.ErrInvalidInput("type must be 'text' or 'voice'")
	}
	name, err := httpx.CleanText("name", name, MaxChannelNameLen, true)
	if err != nil {
		return nil, err
	}
	topic, err = httpx.CleanText("topic", topic, MaxTopicLen, false)
	if err != nil {
		return nil, err
	}
	if categoryID != nil {
		var exists bool
		if err := p.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM categories WHERE id = $1)`, *categoryID).Scan(&exists); err != nil {
			return nil, err
		}
		if !exists {
			return nil, httpx.ErrInvalidInput("category not found")
		}
	}
	var ch Channel
	err = p.QueryRow(ctx, `
		INSERT INTO channels (category_id, name, type, topic, sort_order) VALUES ($1, $2, $3, $4, $5)
		RETURNING id, number, category_id, name, type, topic, sort_order, created_at`,
		categoryID, name, chType, topic, sortOrder).
		Scan(&ch.ID, &ch.Number, &ch.CategoryID, &ch.Name, &ch.Type, &ch.Topic, &ch.SortOrder, &ch.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("create channel: %w", err)
	}
	return &ch, nil
}

// DeleteChannel removes a channel and, by cascade, its messages. It returns
// the storage keys of the media attached to those messages.
func DeleteChannel(ctx context.Context, p *db.Pool, channelID uuid.UUID) ([]string, error) {
	var keys []string
	err := pgx.BeginFunc(ctx, p, func(tx pgx.Tx) error {
		// Locking the channel first blocks concurrent posts and uploads into
		// it (their foreign keys need a share lock on it) until we are done,
		// so every attachment the cascade removes is in keys.
		var one int
		err := tx.QueryRow(ctx, `SELECT 1 FROM channels WHERE id = $1 FOR UPDATE`, channelID).Scan(&one)
		if errors.Is(err, pgx.ErrNoRows) {
			return errChannelNotFound
		}
		if err != nil {
			return fmt.Errorf("lock channel: %w", err)
		}
		if err := collectKeys(ctx, tx, &keys, `
			SELECT s3_key FROM media WHERE message_id IN
				(SELECT id FROM messages WHERE channel_id = $1)`, channelID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM channels WHERE id = $1`, channelID); err != nil {
			return fmt.Errorf("delete channel: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return keys, nil
}

// DeleteCategory removes a category; its channels become uncategorized.
func DeleteCategory(ctx context.Context, p *db.Pool, categoryID uuid.UUID) error {
	tag, err := p.Exec(ctx, `DELETE FROM categories WHERE id = $1`, categoryID)
	if err != nil {
		return fmt.Errorf("delete category: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return httpx.ErrNotFound("category not found")
	}
	return nil
}

var errCategoryNotFound = httpx.ErrNotFound("category not found")

// UpdateChannel renames a channel and/or changes its topic; nil keeps a field.
func UpdateChannel(ctx context.Context, p *db.Pool, id uuid.UUID, name, topic *string) (*Channel, error) {
	if name != nil {
		n, err := httpx.CleanText("name", *name, MaxChannelNameLen, true)
		if err != nil {
			return nil, err
		}
		name = &n
	}
	if topic != nil {
		t, err := httpx.CleanText("topic", *topic, MaxTopicLen, false)
		if err != nil {
			return nil, err
		}
		topic = &t
	}
	var ch Channel
	err := p.QueryRow(ctx, `
		UPDATE channels SET name = COALESCE($2, name), topic = COALESCE($3, topic)
		WHERE id = $1
		RETURNING id, number, category_id, name, type, topic, sort_order, created_at`, id, name, topic).
		Scan(&ch.ID, &ch.Number, &ch.CategoryID, &ch.Name, &ch.Type, &ch.Topic, &ch.SortOrder, &ch.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errChannelNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("update channel: %w", err)
	}
	return &ch, nil
}

// RenameCategory changes a category's name.
func RenameCategory(ctx context.Context, p *db.Pool, id uuid.UUID, name string) error {
	name, err := httpx.CleanText("name", name, MaxChannelNameLen, true)
	if err != nil {
		return err
	}
	tag, err := p.Exec(ctx, `UPDATE categories SET name = $2 WHERE id = $1`, id, name)
	if err != nil {
		return fmt.Errorf("rename category: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return errCategoryNotFound
	}
	return nil
}

// CategoryOrder and ChannelPlacement describe a drag-and-drop result.
type CategoryOrder struct {
	ID        uuid.UUID `json:"id" format:"uuid"`
	SortOrder int       `json:"sort_order"`
}

type ChannelPlacement struct {
	ID         uuid.UUID  `json:"id" format:"uuid"`
	CategoryID *uuid.UUID `json:"category_id" format:"uuid" extensions:"x-nullable"`
	SortOrder  int        `json:"sort_order"`
}

// ApplyLayout stores category order and channel placement in one
// transaction; an unknown category or channel rejects the whole change.
func ApplyLayout(ctx context.Context, p *db.Pool, cats []CategoryOrder, chans []ChannelPlacement) error {
	return pgx.BeginFunc(ctx, p, func(tx pgx.Tx) error {
		if err := lockChannelLayout(ctx, tx); err != nil {
			return err
		}
		for _, c := range cats {
			tag, err := tx.Exec(ctx, `UPDATE categories SET sort_order = $2 WHERE id = $1`, c.ID, c.SortOrder)
			if err != nil {
				return fmt.Errorf("order category: %w", err)
			}
			if tag.RowsAffected() == 0 {
				return errCategoryNotFound
			}
		}
		for _, c := range chans {
			tag, err := tx.Exec(ctx, `
				UPDATE channels SET category_id = $2, sort_order = $3
				WHERE id = $1 AND ($2::uuid IS NULL OR EXISTS (SELECT 1 FROM categories WHERE id = $2))`,
				c.ID, c.CategoryID, c.SortOrder)
			if err != nil {
				return fmt.Errorf("place channel: %w", err)
			}
			if tag.RowsAffected() == 0 {
				return httpx.ErrNotFound("channel or category not found")
			}
		}
		return nil
	})
}

// channelLayoutLockID is the transaction-scoped advisory lock that serialises
// changes to channel placement (layout drags and duplications), so they never
// interleave and cannot deadlock on each other's row locks.
const channelLayoutLockID = 0x6d6e6c6179 // "mnlay"

func lockChannelLayout(ctx context.Context, tx pgx.Tx) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, channelLayoutLockID); err != nil {
		return fmt.Errorf("lock channel layout: %w", err)
	}
	return nil
}

// DuplicateChannel copies a channel's own properties (name, type, topic,
// category) into a new channel placed directly below it in the same group
// (its category, or the uncategorized channels). Messages, reactions and
// per-user state (read position, notification level) are not copied. Channel
// names need not be unique, so the copy keeps the source name unchanged.
func DuplicateChannel(ctx context.Context, p *db.Pool, sourceID uuid.UUID) (*Channel, error) {
	var ch Channel
	err := pgx.BeginFunc(ctx, p, func(tx pgx.Tx) error {
		if err := lockChannelLayout(ctx, tx); err != nil {
			return err
		}
		var src Channel
		err := tx.QueryRow(ctx, `
			SELECT id, category_id, name, type, topic, sort_order
			FROM channels WHERE id = $1 FOR UPDATE`, sourceID).
			Scan(&src.ID, &src.CategoryID, &src.Name, &src.Type, &src.Topic, &src.SortOrder)
		if errors.Is(err, pgx.ErrNoRows) {
			return errChannelNotFound
		}
		if err != nil {
			return fmt.Errorf("lock channel: %w", err)
		}

		// The source's group, in the order GET /api/channels lists it.
		ids, orders, err := lockChannelGroup(ctx, tx, src.CategoryID)
		if err != nil {
			return err
		}
		srcIdx := -1
		for i, id := range ids {
			if id == src.ID {
				srcIdx = i
				break
			}
		}
		if srcIdx < 0 { // cannot happen: the source row is locked
			return errChannelNotFound
		}

		newOrder, shifted := placeBelow(orders, srcIdx)
		batch := &pgx.Batch{}
		for i, order := range shifted {
			batch.Queue(`UPDATE channels SET sort_order = $2 WHERE id = $1`, ids[srcIdx+1+i], order)
		}
		if batch.Len() > 0 {
			if err := tx.SendBatch(ctx, batch).Close(); err != nil {
				return fmt.Errorf("shift channels: %w", err)
			}
		}

		err = tx.QueryRow(ctx, `
			INSERT INTO channels (category_id, name, type, topic, sort_order) VALUES ($1, $2, $3, $4, $5)
			RETURNING id, number, category_id, name, type, topic, sort_order, created_at`,
			src.CategoryID, src.Name, src.Type, src.Topic, newOrder).
			Scan(&ch.ID, &ch.Number, &ch.CategoryID, &ch.Name, &ch.Type, &ch.Topic, &ch.SortOrder, &ch.CreatedAt)
		if err != nil {
			return fmt.Errorf("duplicate channel: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &ch, nil
}

// lockChannelGroup locks the channels of one category (nil: the
// uncategorized ones) and returns their IDs and sort orders in display order.
func lockChannelGroup(ctx context.Context, tx pgx.Tx, categoryID *uuid.UUID) ([]uuid.UUID, []int, error) {
	rows, err := tx.Query(ctx, `
		SELECT id, sort_order FROM channels
		WHERE category_id IS NOT DISTINCT FROM $1
		ORDER BY sort_order, created_at, id
		FOR UPDATE`, categoryID)
	if err != nil {
		return nil, nil, fmt.Errorf("lock channel group: %w", err)
	}
	defer rows.Close()
	var ids []uuid.UUID
	var orders []int
	for rows.Next() {
		var id uuid.UUID
		var order int
		if err := rows.Scan(&id, &order); err != nil {
			return nil, nil, err
		}
		ids = append(ids, id)
		orders = append(orders, order)
	}
	return ids, orders, rows.Err()
}

// placeBelow computes the sort order of a channel inserted directly after
// orders[src], where orders are a group's sort orders in display order
// (sort_order, then created_at). It returns the new channel's sort order and
// the new sort orders of every channel after src: each moves down by one, and
// further where needed so it still sorts strictly after its predecessor (a
// tie would be broken by created_at, putting an older channel above the copy).
func placeBelow(orders []int, src int) (int, []int) {
	newOrder := orders[src] + 1
	shifted := make([]int, 0, len(orders)-src-1)
	prev := newOrder
	for _, o := range orders[src+1:] {
		next := max(o+1, prev+1)
		shifted = append(shifted, next)
		prev = next
	}
	return newOrder, shifted
}
