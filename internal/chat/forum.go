package chat

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/db"
)

const (
	MaxPostTitleLen     = 100
	MaxPostTags         = 5
	MaxForumTags        = 20
	MaxForumTagNameLen  = 20
	MaxForumTagEmojiLen = 32
)

// ForumTag is an admin-defined tag for labeling forum posts in a forum channel.
type ForumTag struct {
	ID        uuid.UUID `json:"id" format:"uuid"`
	ChannelID uuid.UUID `json:"channel_id" format:"uuid"`
	Name      string    `json:"name" maxLength:"20"`
	Emoji     string    `json:"emoji,omitempty" binding:"optional"`
	CreatedAt time.Time `json:"created_at" format:"date-time"`
}

// loadForumPostDetails enriches messages with forum post titles, lock states,
// tags, and last activity timestamps.
func loadForumPostDetails(ctx context.Context, p *db.Pool, msgs []Message, idx map[uuid.UUID]int, ids []uuid.UUID) error {
	if err := loadPostTitlesAndLocks(ctx, p, msgs, idx, ids); err != nil {
		return err
	}
	return loadPostTags(ctx, p, msgs, idx, ids)
}

func loadPostTitlesAndLocks(ctx context.Context, p *db.Pool, msgs []Message, idx map[uuid.UUID]int, ids []uuid.UUID) error {
	rows, err := p.Query(ctx, `
		SELECT m.id, m.title, m.is_locked,
		       (SELECT MAX(r.created_at) FROM messages r WHERE r.parent_id = m.id) AS last_reply_at
		FROM messages m
		WHERE m.id = ANY($1) AND m.parent_id IS NULL AND (m.title IS NOT NULL OR m.is_locked)`, ids)
	if err != nil {
		return fmt.Errorf("load forum post details: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var title *string
		var isLocked bool
		var lastReplyAt *time.Time
		if err := rows.Scan(&id, &title, &isLocked, &lastReplyAt); err != nil {
			return err
		}
		i, ok := idx[id]
		if !ok {
			continue
		}
		msgs[i].Title = title
		msgs[i].IsLocked = isLocked
		if lastReplyAt != nil {
			msgs[i].LastActivityAt = lastReplyAt
		} else {
			msgs[i].LastActivityAt = &msgs[i].CreatedAt
		}
		if msgs[i].Title != nil && msgs[i].Tags == nil {
			msgs[i].Tags = make([]ForumTag, 0)
		}
	}
	return rows.Err()
}

func loadPostTags(ctx context.Context, p *db.Pool, msgs []Message, idx map[uuid.UUID]int, ids []uuid.UUID) error {
	rows, err := p.Query(ctx, `
		SELECT pt.message_id, ft.id, ft.channel_id, ft.name, ft.emoji, ft.created_at
		FROM forum_post_tags pt
		JOIN forum_tags ft ON ft.id = pt.tag_id
		WHERE pt.message_id = ANY($1)
		ORDER BY ft.name`, ids)
	if err != nil {
		return fmt.Errorf("load forum post tags: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var msgID uuid.UUID
		var tag ForumTag
		if err := rows.Scan(&msgID, &tag.ID, &tag.ChannelID, &tag.Name, &tag.Emoji, &tag.CreatedAt); err != nil {
			return err
		}
		if i, ok := idx[msgID]; ok {
			if msgs[i].Tags == nil {
				msgs[i].Tags = make([]ForumTag, 0)
			}
			msgs[i].Tags = append(msgs[i].Tags, tag)
		}
	}
	return rows.Err()
}
