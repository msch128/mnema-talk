package chat

import (
	"context"
	"errors"
	"fmt"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

var (
	errForumTagNotFound  = httpx.ErrNotFound("forum tag not found")
	errTooManyForumTags  = httpx.ErrConflict("a forum channel can have at most 20 tags")
	errTooManyPostTags   = httpx.ErrInvalidInput("a post can have at most 5 tags")
	errTagWrongChannel   = httpx.ErrInvalidInput("tag does not belong to this channel")
	errTagNameLength     = httpx.ErrInvalidInput("tag name must be between 1 and 20 characters")
	errTagEmojiLength    = httpx.ErrInvalidInput("tag emoji cannot exceed 32 characters")
	errPostTitleRequired = httpx.ErrInvalidInput("title is required for forum posts")
	errPostTitleLength   = httpx.ErrInvalidInput("title cannot exceed 100 characters")
)

// CleanTagName validates and sanitizes a forum tag name.
func CleanTagName(name string) (string, error) {
	cleaned, err := httpx.CleanText("name", name, MaxForumTagNameLen, true)
	if err != nil {
		return "", err
	}
	if utf8.RuneCountInString(cleaned) < 1 || utf8.RuneCountInString(cleaned) > MaxForumTagNameLen {
		return "", errTagNameLength
	}
	return cleaned, nil
}

// CleanTagEmoji validates and sanitizes an optional tag emoji.
func CleanTagEmoji(emoji string) (string, error) {
	if emoji == "" {
		return "", nil
	}
	if len(emoji) > MaxForumTagEmojiLen {
		return "", errTagEmojiLength
	}
	return emoji, nil
}

// ListForumTags loads all tags defined in channelID, ordered by name.
func ListForumTags(ctx context.Context, q db.Querier, channelID uuid.UUID) ([]ForumTag, error) {
	rows, err := q.Query(ctx, `
		SELECT id, channel_id, name, emoji, created_at
		FROM forum_tags WHERE channel_id = $1 ORDER BY name ASC`, channelID)
	if err != nil {
		return nil, fmt.Errorf("list forum tags: %w", err)
	}
	defer rows.Close()
	tags := make([]ForumTag, 0)
	for rows.Next() {
		var t ForumTag
		if err := rows.Scan(&t.ID, &t.ChannelID, &t.Name, &t.Emoji, &t.CreatedAt); err != nil {
			return nil, err
		}
		tags = append(tags, t)
	}
	return tags, rows.Err()
}

// CreateForumTag adds a new tag to channelID, enforcing the 20-tag cap.
func CreateForumTag(ctx context.Context, p *db.Pool, channelID uuid.UUID, name, emoji string) (*ForumTag, error) {
	cleanName, err := CleanTagName(name)
	if err != nil {
		return nil, err
	}
	cleanEmoji, err := CleanTagEmoji(emoji)
	if err != nil {
		return nil, err
	}

	var tag ForumTag
	err = pgx.BeginFunc(ctx, p, func(tx pgx.Tx) error {
		var count int
		if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM forum_tags WHERE channel_id = $1`, channelID).Scan(&count); err != nil {
			return err
		}
		if count >= MaxForumTags {
			return errTooManyForumTags
		}
		return tx.QueryRow(ctx, `
			INSERT INTO forum_tags (channel_id, name, emoji) VALUES ($1, $2, $3)
			RETURNING id, channel_id, name, emoji, created_at`,
			channelID, cleanName, cleanEmoji).Scan(&tag.ID, &tag.ChannelID, &tag.Name, &tag.Emoji, &tag.CreatedAt)
	})
	if err != nil {
		return nil, err
	}
	return &tag, nil
}

// UpdateForumTag updates an existing tag's name and/or emoji.
func UpdateForumTag(ctx context.Context, q db.Querier, channelID, tagID uuid.UUID, name, emoji *string) (*ForumTag, error) {
	if name == nil && emoji == nil {
		return nil, httpx.ErrInvalidInput("no fields to update")
	}
	var tag ForumTag
	err := q.QueryRow(ctx, `SELECT id, channel_id, name, emoji, created_at FROM forum_tags WHERE id = $1 AND channel_id = $2`, tagID, channelID).
		Scan(&tag.ID, &tag.ChannelID, &tag.Name, &tag.Emoji, &tag.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errForumTagNotFound
	}
	if err != nil {
		return nil, err
	}

	if name != nil {
		cleanName, err := CleanTagName(*name)
		if err != nil {
			return nil, err
		}
		tag.Name = cleanName
	}
	if emoji != nil {
		cleanEmoji, err := CleanTagEmoji(*emoji)
		if err != nil {
			return nil, err
		}
		tag.Emoji = cleanEmoji
	}

	err = q.QueryRow(ctx, `
		UPDATE forum_tags SET name = $3, emoji = $4 WHERE id = $1 AND channel_id = $2
		RETURNING id, channel_id, name, emoji, created_at`,
		tagID, channelID, tag.Name, tag.Emoji).Scan(&tag.ID, &tag.ChannelID, &tag.Name, &tag.Emoji, &tag.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("update forum tag: %w", err)
	}
	return &tag, nil
}

// DeleteForumTag removes a tag from the channel.
func DeleteForumTag(ctx context.Context, q db.Querier, channelID, tagID uuid.UUID) error {
	res, err := q.Exec(ctx, `DELETE FROM forum_tags WHERE id = $1 AND channel_id = $2`, tagID, channelID)
	if err != nil {
		return fmt.Errorf("delete forum tag: %w", err)
	}
	if res.RowsAffected() == 0 {
		return errForumTagNotFound
	}
	return nil
}

// ValidatePostTags verifies that tagIDs are at most 5 and all belong to channelID.
func ValidatePostTags(ctx context.Context, q db.Querier, channelID uuid.UUID, tagIDs []uuid.UUID) error {
	if len(tagIDs) == 0 {
		return nil
	}
	if len(tagIDs) > MaxPostTags {
		return errTooManyPostTags
	}
	// Ensure all tag IDs belong to this channel
	var count int
	err := q.QueryRow(ctx, `SELECT COUNT(*) FROM forum_tags WHERE channel_id = $1 AND id = ANY($2)`, channelID, tagIDs).Scan(&count)
	if err != nil {
		return fmt.Errorf("validate post tags: %w", err)
	}
	if count != len(tagIDs) {
		return errTagWrongChannel
	}
	return nil
}
