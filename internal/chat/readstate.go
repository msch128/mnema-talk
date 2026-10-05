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
	"github.com/msch128/mnema-talk/internal/httpx"
)

// NotifyLevel says which new messages of a channel may notify a user.
type NotifyLevel string

const (
	NotifyAll      NotifyLevel = "all"
	NotifyMentions NotifyLevel = "mentions"
	NotifyMute     NotifyLevel = "mute"
)

func (l NotifyLevel) valid() bool {
	return l == NotifyAll || l == NotifyMentions || l == NotifyMute
}

// ReadState is one text channel's unread summary for one user.
type ReadState struct {
	ChannelID    uuid.UUID   `json:"channel_id"`
	UnreadCount  int         `json:"unread_count"`
	MentionCount int         `json:"mention_count"`
	LastReadAt   *time.Time  `json:"last_read_at"`
	NotifyLevel  NotifyLevel `json:"notify_level"`
}

// GetReadStates returns the unread summary of every text channel for user.
// Unread counts top-level messages by others since the last read; mentions
// count any message (also thread replies) that mentions the user (@username,
// @all, @here; see message_mentions) or replies to one of their messages. Before the first read, "last read" is the sign-up.
func GetReadStates(ctx context.Context, p *db.Pool, user *auth.User) ([]ReadState, error) {
	rows, err := p.Query(ctx, `
		SELECT c.id,
		       count(m.id) FILTER (WHERE m.parent_id IS NULL),
		       count(m.id) FILTER (WHERE orig.user_id = $1 OR EXISTS (
		           SELECT 1 FROM message_mentions mm WHERE mm.message_id = m.id AND mm.user_id = $1)),
		       cr.last_read_at,
		       COALESCE(cr.notify_level, 'all')
		FROM channels c
		JOIN users me ON me.id = $1
		LEFT JOIN channel_reads cr ON cr.channel_id = c.id AND cr.user_id = $1
		LEFT JOIN messages m ON m.channel_id = c.id AND m.user_id <> $1
		     AND m.created_at > COALESCE(cr.last_read_at, me.created_at)
		LEFT JOIN messages orig ON orig.id = m.reply_to_id
		WHERE c.type = 'text'
		GROUP BY c.id, cr.last_read_at, cr.notify_level
		ORDER BY c.id`, user.ID)
	if err != nil {
		return nil, fmt.Errorf("read states: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (ReadState, error) {
		var s ReadState
		err := row.Scan(&s.ChannelID, &s.UnreadCount, &s.MentionCount, &s.LastReadAt, &s.NotifyLevel)
		return s, err
	})
}

// messageTime returns when messageID was posted in channelID.
func messageTime(ctx context.Context, p *db.Pool, channelID, messageID uuid.UUID) (time.Time, error) {
	var at time.Time
	err := p.QueryRow(ctx, `SELECT created_at FROM messages WHERE id = $1 AND channel_id = $2`, messageID, channelID).Scan(&at)
	if errors.Is(err, pgx.ErrNoRows) {
		return at, httpx.ErrNotFound("message not found in this channel")
	}
	return at, err
}

// MarkRead moves the user's read marker forward to messageID (or to now). It
// never moves backwards; use MarkUnread for that.
func MarkRead(ctx context.Context, p *db.Pool, userID, channelID uuid.UUID, messageID *uuid.UUID) (time.Time, error) {
	at := time.Now()
	if messageID != nil {
		t, err := messageTime(ctx, p, channelID, *messageID)
		if err != nil {
			return at, err
		}
		at = t
	}
	err := p.QueryRow(ctx, `
		INSERT INTO channel_reads (user_id, channel_id, last_read_at) VALUES ($1, $2, $3)
		ON CONFLICT (user_id, channel_id) DO UPDATE
		SET last_read_at = GREATEST(COALESCE(channel_reads.last_read_at, EXCLUDED.last_read_at), EXCLUDED.last_read_at)
		RETURNING last_read_at`, userID, channelID, at).Scan(&at)
	return at, err
}

// MarkUnread sets the read marker just before messageID, so it and
// everything after it count as unread again.
func MarkUnread(ctx context.Context, p *db.Pool, userID, channelID, messageID uuid.UUID) (time.Time, error) {
	t, err := messageTime(ctx, p, channelID, messageID)
	if err != nil {
		return t, err
	}
	at := t.Add(-time.Microsecond)
	_, err = p.Exec(ctx, `
		INSERT INTO channel_reads (user_id, channel_id, last_read_at) VALUES ($1, $2, $3)
		ON CONFLICT (user_id, channel_id) DO UPDATE SET last_read_at = EXCLUDED.last_read_at`,
		userID, channelID, at)
	return at, err
}

// SetNotifyLevel stores how loudly channelID may notify the user.
func SetNotifyLevel(ctx context.Context, p *db.Pool, userID, channelID uuid.UUID, level NotifyLevel) error {
	if !level.valid() {
		return httpx.ErrInvalidInput("level must be all, mentions or mute")
	}
	_, err := p.Exec(ctx, `
		INSERT INTO channel_reads (user_id, channel_id, notify_level) VALUES ($1, $2, $3)
		ON CONFLICT (user_id, channel_id) DO UPDATE SET notify_level = EXCLUDED.notify_level`,
		userID, channelID, level)
	return err
}
