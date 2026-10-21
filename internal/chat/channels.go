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
	// UserLimit is the most members a voice channel admits at once; 0 = no
	// limit.
	UserLimit int `json:"user_limit" minimum:"0" maximum:"999"`
	// SlowModeSeconds is the least time between two messages of one member
	// in this channel; 0 = off. Members who may manage messages here are
	// exempt.
	SlowModeSeconds int `json:"slow_mode_seconds" minimum:"0" maximum:"21600"`
}

type Category struct {
	ID        uuid.UUID `json:"id" format:"uuid"`
	Name      string    `json:"name" maxLength:"64"`
	SortOrder int       `json:"sort_order"`
	Channels  []Channel `json:"channels"`
	CreatedAt time.Time `json:"created_at" format:"date-time"`
}

func loadCategories(ctx context.Context, p *db.Pool) ([]Category, map[uuid.UUID]int, error) {
	catRows, err := p.Query(ctx, `SELECT id, name, sort_order, created_at FROM categories ORDER BY sort_order, created_at`)
	if err != nil {
		return nil, nil, fmt.Errorf("query categories: %w", err)
	}
	defer catRows.Close()
	categories := make([]Category, 0)
	categoryIdx := make(map[uuid.UUID]int)
	for catRows.Next() {
		cat := Category{Channels: make([]Channel, 0)}
		if err := catRows.Scan(&cat.ID, &cat.Name, &cat.SortOrder, &cat.CreatedAt); err != nil {
			return nil, nil, err
		}
		categoryIdx[cat.ID] = len(categories)
		categories = append(categories, cat)
	}
	return categories, categoryIdx, catRows.Err()
}

func populateChannels(ctx context.Context, p *db.Pool, categories []Category, categoryIdx map[uuid.UUID]int) ([]Channel, error) {
	chanRows, err := p.Query(ctx, `
		SELECT id, number, category_id, name, type, topic, sort_order, created_at, user_limit, slow_mode_seconds
		FROM channels ORDER BY sort_order, created_at`)
	if err != nil {
		return nil, fmt.Errorf("query channels: %w", err)
	}
	defer chanRows.Close()

	uncategorized := make([]Channel, 0)
	for chanRows.Next() {
		var ch Channel
		if err := chanRows.Scan(&ch.ID, &ch.Number, &ch.CategoryID, &ch.Name, &ch.Type, &ch.Topic, &ch.SortOrder, &ch.CreatedAt, &ch.UserLimit, &ch.SlowModeSeconds); err != nil {
			return nil, err
		}
		if ch.CategoryID != nil {
			if idx, ok := categoryIdx[*ch.CategoryID]; ok {
				categories[idx].Channels = append(categories[idx].Channels, ch)
				continue
			}
		}
		uncategorized = append(uncategorized, ch)
	}
	return uncategorized, chanRows.Err()
}

// GetServerHierarchy returns all categories with their nested channels, plus
// uncategorized channels.
func GetServerHierarchy(ctx context.Context, p *db.Pool) ([]Category, []Channel, error) {
	categories, categoryIdx, err := loadCategories(ctx, p)
	if err != nil {
		return nil, nil, err
	}
	uncategorized, err := populateChannels(ctx, p, categories, categoryIdx)
	if err != nil {
		return nil, nil, err
	}
	return categories, uncategorized, nil
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

func cleanChannelInputs(name, topic string, chType ChannelType) (string, string, error) {
	if chType != ChannelTypeText && chType != ChannelTypeVoice {
		return "", "", httpx.ErrInvalidInput("type must be 'text' or 'voice'")
	}
	cleanName, err := httpx.CleanText("name", name, MaxChannelNameLen, true)
	if err != nil {
		return "", "", err
	}
	cleanTopic, err := httpx.CleanText("topic", topic, MaxTopicLen, false)
	if err != nil {
		return "", "", err
	}
	return cleanName, cleanTopic, nil
}

func verifyCategoryExists(ctx context.Context, p *db.Pool, categoryID *uuid.UUID) error {
	if categoryID == nil {
		return nil
	}
	var exists bool
	if err := p.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM categories WHERE id = $1)`, *categoryID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return httpx.ErrInvalidInput("category not found")
	}
	return nil
}

func CreateChannel(ctx context.Context, p *db.Pool, categoryID *uuid.UUID, name string, chType ChannelType, topic string, sortOrder int) (*Channel, error) {
	name, topic, err := cleanChannelInputs(name, topic, chType)
	if err != nil {
		return nil, err
	}
	if err := verifyCategoryExists(ctx, p, categoryID); err != nil {
		return nil, err
	}
	var ch Channel
	err = p.QueryRow(ctx, `
		INSERT INTO channels (category_id, name, type, topic, sort_order) VALUES ($1, $2, $3, $4, $5)
		RETURNING id, number, category_id, name, type, topic, sort_order, created_at, user_limit, slow_mode_seconds`,
		categoryID, name, chType, topic, sortOrder).
		Scan(&ch.ID, &ch.Number, &ch.CategoryID, &ch.Name, &ch.Type, &ch.Topic, &ch.SortOrder, &ch.CreatedAt, &ch.UserLimit, &ch.SlowModeSeconds)
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
	return pgx.BeginFunc(ctx, p, func(tx pgx.Tx) error { return DeleteCategoryTx(ctx, tx, categoryID) })
}

func checkCategoryMergeable(ctx context.Context, tx pgx.Tx, categoryID uuid.UUID) error {
	var inexact bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM permission_overwrites WHERE category_id = $1)
		   AND EXISTS (SELECT 1 FROM permission_overwrites o JOIN channels ch ON ch.id = o.channel_id WHERE ch.category_id = $1)`,
		categoryID).Scan(&inexact); err != nil {
		return fmt.Errorf("check category permissions: %w", err)
	}
	if inexact {
		return httpx.ErrConflict("clear the permissions of the channels in this category, or of the category, first: they cannot be merged exactly")
	}
	return nil
}

func copyCategoryOverwrites(ctx context.Context, tx pgx.Tx, categoryID uuid.UUID) error {
	if _, err := tx.Exec(ctx, `
		INSERT INTO permission_overwrites (channel_id, role_id, allow, deny)
		SELECT ch.id, co.role_id, co.allow, co.deny
		FROM channels ch JOIN permission_overwrites co ON co.category_id = ch.category_id
		WHERE ch.category_id = $1 AND co.role_id IS NOT NULL`, categoryID); err != nil {
		return fmt.Errorf("keep category role permissions: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO permission_overwrites (channel_id, user_id, allow, deny)
		SELECT ch.id, co.user_id, co.allow, co.deny
		FROM channels ch JOIN permission_overwrites co ON co.category_id = ch.category_id
		WHERE ch.category_id = $1 AND co.user_id IS NOT NULL`, categoryID); err != nil {
		return fmt.Errorf("keep category user permissions: %w", err)
	}
	return nil
}

// DeleteCategoryTx is DeleteCategory inside a transaction. The category's
// permission overwrites do not vanish for its channels: they are copied to
// each channel first, so deleting a private category never makes its channels
// public. That copy is only exact when the channels have no overwrites of
// their own (two layers do not always flatten into one); otherwise the
// deletion is refused with 409 until one side is cleared.
func DeleteCategoryTx(ctx context.Context, tx pgx.Tx, categoryID uuid.UUID) error {
	var one int
	err := tx.QueryRow(ctx, `SELECT 1 FROM categories WHERE id = $1 FOR UPDATE`, categoryID).Scan(&one)
	if errors.Is(err, pgx.ErrNoRows) {
		return errCategoryNotFound
	}
	if err != nil {
		return fmt.Errorf("lock category: %w", err)
	}
	if err := checkCategoryMergeable(ctx, tx, categoryID); err != nil {
		return err
	}
	if err := copyCategoryOverwrites(ctx, tx, categoryID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM categories WHERE id = $1`, categoryID); err != nil {
		return fmt.Errorf("delete category: %w", err)
	}
	return nil
}

var errCategoryNotFound = httpx.ErrNotFound("category not found")

// MaxUserLimit is the highest member limit a voice channel can have.
const MaxUserLimit = 999

// MaxSlowModeSeconds is the longest slow mode: six hours.
const MaxSlowModeSeconds = 21600

// UpdateChannel renames a channel, changes its topic, its member limit
// (voice channels only; 0 removes it) and/or its slow mode (0 turns it off);
// nil keeps a field.
func UpdateChannel(ctx context.Context, p *db.Pool, id uuid.UUID, name, topic *string, userLimit, slowMode *int) (*Channel, error) {
	name, topic, err := validateChannelEdit(name, topic, userLimit, slowMode)
	if err != nil {
		return nil, err
	}
	var ch Channel
	err = p.QueryRow(ctx, `
		UPDATE channels SET name = COALESCE($2, name), topic = COALESCE($3, topic),
			user_limit = COALESCE($4, user_limit), slow_mode_seconds = COALESCE($5, slow_mode_seconds)
		WHERE id = $1 AND ($4::int IS NULL OR $4 = 0 OR type = 'voice')
		RETURNING id, number, category_id, name, type, topic, sort_order, created_at, user_limit, slow_mode_seconds`, id, name, topic, userLimit, slowMode).
		Scan(&ch.ID, &ch.Number, &ch.CategoryID, &ch.Name, &ch.Type, &ch.Topic, &ch.SortOrder, &ch.CreatedAt, &ch.UserLimit, &ch.SlowModeSeconds)
	if errors.Is(err, pgx.ErrNoRows) {
		if userLimit != nil && *userLimit > 0 {
			if _, lerr := LoadChannel(ctx, p, id); lerr == nil {
				return nil, httpx.ErrInvalidInput("user_limit applies to voice channels only")
			}
		}
		return nil, errChannelNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("update channel: %w", err)
	}
	return &ch, nil
}

func validateChannelLimits(userLimit, slowMode *int) error {
	if slowMode != nil {
		if *slowMode < 0 || *slowMode > MaxSlowModeSeconds {
			return httpx.ErrInvalidInput(fmt.Sprintf("slow_mode_seconds must be between 0 (off) and %d", MaxSlowModeSeconds))
		}
	}
	if userLimit != nil {
		if *userLimit < 0 || *userLimit > MaxUserLimit {
			return httpx.ErrInvalidInput(fmt.Sprintf("user_limit must be between 0 (no limit) and %d", MaxUserLimit))
		}
	}
	return nil
}

func validateOptionalName(name *string) (*string, error) {
	if name == nil {
		return nil, nil
	}
	n, err := httpx.CleanText("name", *name, MaxChannelNameLen, true)
	if err != nil {
		return nil, err
	}
	return &n, nil
}

func validateOptionalTopic(topic *string) (*string, error) {
	if topic == nil {
		return nil, nil
	}
	t, err := httpx.CleanText("topic", *topic, MaxTopicLen, false)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// validateChannelEdit checks the fields of a channel update and returns the
// cleaned name and topic (nil stays nil).
func validateChannelEdit(name, topic *string, userLimit, slowMode *int) (*string, *string, error) {
	if err := validateChannelLimits(userLimit, slowMode); err != nil {
		return nil, nil, err
	}
	validName, err := validateOptionalName(name)
	if err != nil {
		return nil, nil, err
	}
	validTopic, err := validateOptionalTopic(topic)
	if err != nil {
		return nil, nil, err
	}
	return validName, validTopic, nil
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
