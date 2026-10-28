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

// ReadState is one channel's unread summary for one user.
type ReadState struct {
	ChannelID    uuid.UUID   `json:"channel_id" format:"uuid"`
	UnreadCount  int         `json:"unread_count"`
	MentionCount int         `json:"mention_count"`
	LastReadAt   *time.Time  `json:"last_read_at" format:"date-time" extensions:"x-nullable"`
	NotifyLevel  NotifyLevel `json:"notify_level"`
}

// GetReadStates returns the unread summary of every channel for user (a
// voice channel's chat counts like a text channel).
// Unread counts top-level messages by others since the last read; mentions
// count any message (also thread replies) that mentions the user (@username,
// @all, @here unless the member suppresses them; see message_mentions) or replies to one of their messages. In a
// muted channel only messages addressed to the user count (by name, through a
// role or as a reply; not @all or @here), the same ones that still notify.
// Before the first read, "last read" is the sign-up.
// Messages of members the caller blocked never count as mentions.
// NotifyLevel is the effective level: the channel's own setting, else its
// category's, else the member's server default, else "all"; an expired mute counts as none (see ResolveNotify).
func GetReadStates(ctx context.Context, p *db.Pool, user *auth.User) ([]ReadState, error) {
	rows, err := p.Query(ctx, `
		SELECT c.id,
		       count(m.id) FILTER (WHERE m.parent_id IS NULL),
		       count(m.id) FILTER (WHERE NOT blocked.is_blocked AND (orig.user_id = $1 OR EXISTS (
		           SELECT 1 FROM message_mentions mm WHERE mm.message_id = m.id AND mm.user_id = $1
		             AND (mm.kind <> $2 OR NOT COALESCE(sn.suppress_everyone, FALSE))))),
		       count(m.id) FILTER (WHERE NOT blocked.is_blocked AND (orig.user_id = $1 OR EXISTS (
		           SELECT 1 FROM message_mentions mm WHERE mm.message_id = m.id AND mm.user_id = $1 AND mm.kind <> $2))),
		       cr.last_read_at,
		       COALESCE(cr.notify_level, 'inherit'), cr.muted_until,
		       COALESCE(cn.notify_level, 'inherit'), cn.muted_until,
		       COALESCE(sn.notify_level, 'all'), sn.muted_until,
		       NOW()
		FROM channels c
		JOIN users me ON me.id = $1
		LEFT JOIN channel_reads cr ON cr.channel_id = c.id AND cr.user_id = $1
		LEFT JOIN category_notify cn ON cn.category_id = c.category_id AND cn.user_id = $1
		LEFT JOIN server_notify sn ON sn.user_id = $1
		LEFT JOIN messages m ON m.channel_id = c.id AND m.user_id <> $1
		     AND m.created_at > COALESCE(cr.last_read_at, me.created_at)
		LEFT JOIN messages orig ON orig.id = m.reply_to_id
		CROSS JOIN LATERAL (SELECT EXISTS (SELECT 1 FROM user_blocks ub WHERE ub.blocker_id = $1 AND ub.blocked_id = m.user_id) AS is_blocked) blocked
		WHERE c.type <> 'dm'
		GROUP BY c.id, cr.last_read_at, cr.notify_level, cr.muted_until, cn.notify_level, cn.muted_until, sn.notify_level, sn.muted_until, sn.suppress_everyone
		ORDER BY c.id`, user.ID, MentionKindGroup)
	if err != nil {
		return nil, fmt.Errorf("read states: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (ReadState, error) {
		var s ReadState
		var ch, cat, srv NotifySetting
		var now time.Time
		var addressed int
		err := row.Scan(&s.ChannelID, &s.UnreadCount, &s.MentionCount, &addressed, &s.LastReadAt,
			&ch.Level, &ch.MutedUntil, &cat.Level, &cat.MutedUntil, &srv.Level, &srv.MutedUntil, &now)
		s.NotifyLevel = ResolveNotify(now, ch, cat, srv)
		if s.NotifyLevel == NotifyMute {
			s.MentionCount = addressed
		}
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
//
// It writes only when another member's message sits between the current
// marker and the target, or when the member has no marker yet (the first read
// starts the "new since" divider). Repeated calls (a second tab, Esc on an
// already read channel, a client retry) cost one indexed probe and no row
// version, and sending a message never writes a marker at all. The result is
// the marker afterwards.
func MarkRead(ctx context.Context, p *db.Pool, userID, channelID uuid.UUID, messageID *uuid.UUID) (time.Time, error) {
	// "Now" is the database's clock, the one messages.created_at comes from:
	// the app's clock may lag it by a few milliseconds (separate containers,
	// Docker Desktop's VM), which left the newest message unread.
	var at *time.Time
	if messageID != nil {
		t, err := messageTime(ctx, p, channelID, *messageID)
		if err != nil {
			return time.Time{}, err
		}
		at = &t
	}
	var marked time.Time
	err := p.QueryRow(ctx, `
		WITH marker AS (
		    SELECT last_read_at AS at FROM channel_reads WHERE user_id = $1 AND channel_id = $2
		), cur AS (
		    SELECT COALESCE((SELECT at FROM marker), (SELECT created_at FROM users WHERE id = $1)) AS at,
		           (SELECT at FROM marker) IS NOT NULL AS has_marker
		), target AS (SELECT COALESCE($3::timestamptz, NOW()) AS at),
		written AS (
		    INSERT INTO channel_reads (user_id, channel_id, last_read_at, notify_level)
		    SELECT $1, $2, target.at, 'inherit' FROM target, cur
		    WHERE NOT cur.has_marker OR EXISTS (SELECT 1 FROM messages m
		                  WHERE m.channel_id = $2 AND m.user_id <> $1
		                    AND m.created_at > cur.at AND m.created_at <= target.at)
		    ON CONFLICT (user_id, channel_id) DO UPDATE
		    SET last_read_at = GREATEST(COALESCE(channel_reads.last_read_at, EXCLUDED.last_read_at), EXCLUDED.last_read_at)
		    RETURNING last_read_at
		)
		SELECT COALESCE((SELECT last_read_at FROM written), (SELECT at FROM cur))`,
		userID, channelID, at).Scan(&marked)
	return marked, err
}

// FirstUnread returns the oldest top-level message by another member posted
// after since in channelID, or nil when there is none. The client uses it to
// jump to the first unread message when that one is older than the page it
// has loaded; the divider itself is computed from the loaded messages.
func FirstUnread(ctx context.Context, p *db.Pool, userID, channelID uuid.UUID, since time.Time) (*uuid.UUID, error) {
	var id uuid.UUID
	err := p.QueryRow(ctx, `
		SELECT id FROM messages
		WHERE channel_id = $1 AND parent_id IS NULL AND user_id <> $2 AND created_at > $3
		ORDER BY created_at, id LIMIT 1`, channelID, userID, since).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("first unread: %w", err)
	}
	return &id, nil
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
		INSERT INTO channel_reads (user_id, channel_id, last_read_at, notify_level) VALUES ($1, $2, $3, 'inherit')
		ON CONFLICT (user_id, channel_id) DO UPDATE SET last_read_at = EXCLUDED.last_read_at`,
		userID, channelID, at)
	return at, err
}
