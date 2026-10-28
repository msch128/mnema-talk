package chat

import (
	"context"
	"errors"
	"fmt"

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
	errTagNameTaken      = httpx.ErrConflict("a tag with this name already exists")
	errPostTitleRequired = httpx.ErrInvalidInput("title is required for forum posts")
)

// CleanTagName validates and sanitizes a forum tag name.
func CleanTagName(name string) (string, error) {
	return httpx.CleanText("name", name, MaxForumTagNameLen, true)
}

// CleanTagEmoji validates and sanitizes an optional tag emoji.
func CleanTagEmoji(emoji string) (string, error) {
	if emoji == "" {
		return "", nil
	}
	return httpx.CleanText("emoji", emoji, MaxForumTagEmojiLen, true)
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
		// Lock the channel row so concurrent creates cannot pass the cap together.
		if _, err := tx.Exec(ctx, `SELECT 1 FROM channels WHERE id = $1 FOR UPDATE`, channelID); err != nil {
			return err
		}
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
	if db.IsUniqueViolation(err) {
		return nil, errTagNameTaken
	}
	if err != nil {
		return nil, err
	}
	return &tag, nil
}

// UpdateForumTag updates an existing tag's name and/or emoji and returns the
// tag as it was before the change next to the new one. A single statement
// writes only the fields that are set, so two admins changing different
// fields of one tag cannot undo each other.
func UpdateForumTag(ctx context.Context, q db.Querier, channelID, tagID uuid.UUID, name, emoji *string) (updated, previous *ForumTag, err error) {
	if name == nil && emoji == nil {
		return nil, nil, httpx.ErrInvalidInput("no fields to update")
	}
	var cleanName, cleanEmoji *string
	if name != nil {
		n, err := CleanTagName(*name)
		if err != nil {
			return nil, nil, err
		}
		cleanName = &n
	}
	if emoji != nil {
		e, err := CleanTagEmoji(*emoji)
		if err != nil {
			return nil, nil, err
		}
		cleanEmoji = &e
	}

	var tag, old ForumTag
	err = q.QueryRow(ctx, `
		UPDATE forum_tags t SET name = COALESCE($3, o.name), emoji = COALESCE($4, o.emoji)
		FROM (SELECT id, name, emoji FROM forum_tags WHERE id = $1 AND channel_id = $2 FOR UPDATE) o
		WHERE t.id = o.id
		RETURNING t.id, t.channel_id, t.name, t.emoji, t.created_at, o.name, o.emoji`,
		tagID, channelID, cleanName, cleanEmoji).
		Scan(&tag.ID, &tag.ChannelID, &tag.Name, &tag.Emoji, &tag.CreatedAt, &old.Name, &old.Emoji)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, nil, errForumTagNotFound
	case db.IsUniqueViolation(err):
		return nil, nil, errTagNameTaken
	case err != nil:
		return nil, nil, fmt.Errorf("update forum tag: %w", err)
	}
	old.ID, old.ChannelID, old.CreatedAt = tag.ID, tag.ChannelID, tag.CreatedAt
	return &tag, &old, nil
}

// DeleteForumTag removes a tag from the channel and returns what it was.
func DeleteForumTag(ctx context.Context, q db.Querier, channelID, tagID uuid.UUID) (*ForumTag, error) {
	var tag ForumTag
	err := q.QueryRow(ctx, `
		DELETE FROM forum_tags WHERE id = $1 AND channel_id = $2
		RETURNING id, channel_id, name, emoji, created_at`, tagID, channelID).
		Scan(&tag.ID, &tag.ChannelID, &tag.Name, &tag.Emoji, &tag.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errForumTagNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("delete forum tag: %w", err)
	}
	return &tag, nil
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
