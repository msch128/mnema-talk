package chat

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// NotifyLevel says which new messages of a channel may notify a user.
// "inherit" only exists as a stored setting (follow the category, then "all");
// the effective level a client sees is never "inherit".
type NotifyLevel string

const (
	NotifyAll      NotifyLevel = "all"
	NotifyMentions NotifyLevel = "mentions"
	NotifyMute     NotifyLevel = "mute"
	NotifyInherit  NotifyLevel = "inherit"
)

// maxMuteSeconds bounds a timed mute (366 days).
const maxMuteSeconds = 366 * 24 * 3600

func (l NotifyLevel) valid() bool {
	return l == NotifyAll || l == NotifyMentions || l == NotifyMute || l == NotifyInherit
}

// NotifySetting is one stored setting of a channel or a category.
type NotifySetting struct {
	Level      NotifyLevel `json:"level"`
	MutedUntil *time.Time  `json:"muted_until" format:"date-time" extensions:"x-nullable"`
}

// active reports the setting after expiry: a mute whose end has passed is
// unmuted, i.e. it no longer decides ("inherit"). now must come from the same
// clock as the stored timestamps (the database's).
func (s NotifySetting) active(now time.Time) NotifyLevel {
	switch {
	case s.Level == "":
		return NotifyInherit
	case s.Level == NotifyMute && s.MutedUntil != nil && !s.MutedUntil.After(now):
		return NotifyInherit
	}
	return s.Level
}

// ResolveNotify is the effective level of a channel: its own setting, else its
// category's, else the member's server default, else "all". Expired mutes are
// skipped. Pass the zero NotifySetting as server for conversations, which the
// server default does not reach.
func ResolveNotify(now time.Time, channel, category, server NotifySetting) NotifyLevel {
	for _, s := range [...]NotifySetting{channel, category, server} {
		if l := s.active(now); l != NotifyInherit {
			return l
		}
	}
	return NotifyAll
}

// validateSetting checks a requested level and mute end against the database
// clock (the one the stored timestamps and ResolveNotify use).
func validateSetting(ctx context.Context, p *db.Pool, level NotifyLevel, until *time.Time) error {
	if !level.valid() {
		return httpx.ErrInvalidInput("level must be all, mentions, mute or inherit")
	}
	if until == nil {
		return nil
	}
	if level != NotifyMute {
		return httpx.ErrInvalidInput("muted_until needs level mute")
	}
	var ok bool
	err := p.QueryRow(ctx, `SELECT $1::timestamptz > NOW() AND $1::timestamptz <= NOW() + make_interval(secs => $2)`,
		*until, maxMuteSeconds).Scan(&ok)
	if err != nil {
		return err
	}
	if !ok {
		return httpx.ErrInvalidInput("muted_until must be in the future and within a year")
	}
	return nil
}

// SetNotifyLevel stores how loudly channelID may notify the user; until ends a
// mute (only with level mute; nil keeps it until unmuted).
func SetNotifyLevel(ctx context.Context, p *db.Pool, userID, channelID uuid.UUID, level NotifyLevel, until *time.Time) error {
	if err := validateSetting(ctx, p, level, until); err != nil {
		return err
	}
	_, err := p.Exec(ctx, `
		INSERT INTO channel_reads (user_id, channel_id, notify_level, muted_until) VALUES ($1, $2, $3, $4)
		ON CONFLICT (user_id, channel_id) DO UPDATE
		SET notify_level = EXCLUDED.notify_level, muted_until = EXCLUDED.muted_until`,
		userID, channelID, level, until)
	return err
}

// SetCategoryNotify stores the category-wide setting channels inherit.
func SetCategoryNotify(ctx context.Context, p *db.Pool, userID, categoryID uuid.UUID, level NotifyLevel, until *time.Time) error {
	if err := validateSetting(ctx, p, level, until); err != nil {
		return err
	}
	var exists bool
	if err := p.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM categories WHERE id = $1)`, categoryID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return errCategoryNotFound
	}
	_, err := p.Exec(ctx, `
		INSERT INTO category_notify (user_id, category_id, notify_level, muted_until) VALUES ($1, $2, $3, $4)
		ON CONFLICT (user_id, category_id) DO UPDATE
		SET notify_level = EXCLUDED.notify_level, muted_until = EXCLUDED.muted_until`,
		userID, categoryID, level, until)
	return err
}

// ChannelNotifySetting is a stored channel setting.
type ChannelNotifySetting struct {
	ChannelID uuid.UUID `json:"channel_id" format:"uuid"`
	NotifySetting
}

// CategoryNotifySetting is a stored category setting.
type CategoryNotifySetting struct {
	CategoryID uuid.UUID `json:"category_id" format:"uuid"`
	NotifySetting
}

// NotificationSettings is the caller's stored settings. Only channels and
// categories with a setting that still decides something appear (an expired
// mute is left out); "inherit" is never listed.
type NotificationSettings struct {
	Channels   []ChannelNotifySetting  `json:"channels"`
	Categories []CategoryNotifySetting `json:"categories"`
	// Server is the member's server-wide default (always present).
	Server ServerNotify `json:"server"`
}

// settingRows lists the user's active settings of one table. A nil allowed
// list means no restriction.
func settingRows[T any](ctx context.Context, p *db.Pool, table, idColumn string, userID uuid.UUID, allowed []uuid.UUID) ([]T, error) {
	rows, err := p.Query(ctx, fmt.Sprintf(`
		SELECT %[2]s, notify_level, muted_until FROM %[1]s
		WHERE user_id = $1 AND notify_level <> 'inherit'
		  AND (notify_level <> 'mute' OR muted_until IS NULL OR muted_until > NOW())
		  AND ($2::uuid[] IS NULL OR %[2]s = ANY($2::uuid[]))
		ORDER BY %[2]s`, table, idColumn), userID, allowed)
	if err != nil {
		return nil, fmt.Errorf("%s settings: %w", table, err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowToStructByPos[T])
	if err != nil {
		return nil, fmt.Errorf("%s settings: %w", table, err)
	}
	if out == nil {
		out = []T{}
	}
	return out, nil
}

// GetNotificationSettings lists the user's active settings, limited to the
// channels in visible and the categories in visibleCats (nil: no limit).
func GetNotificationSettings(ctx context.Context, p *db.Pool, userID uuid.UUID, visible, visibleCats []uuid.UUID) (NotificationSettings, error) {
	var out NotificationSettings
	var err error
	if out.Channels, err = settingRows[ChannelNotifySetting](ctx, p, "channel_reads", "channel_id", userID, visible); err != nil {
		return out, err
	}
	if out.Categories, err = settingRows[CategoryNotifySetting](ctx, p, "category_notify", "category_id", userID, visibleCats); err != nil {
		return out, err
	}
	out.Server, err = GetServerNotify(ctx, p, userID)
	return out, err
}

// MarkAllRead moves the read marker of every channel in visible (nil: all) to
// now, optionally only those of one category, in one statement. It never moves
// a marker backwards.
func MarkAllRead(ctx context.Context, p *db.Pool, userID uuid.UUID, visible []uuid.UUID, categoryID *uuid.UUID) error {
	_, err := p.Exec(ctx, `
		INSERT INTO channel_reads (user_id, channel_id, last_read_at, notify_level)
		SELECT $1, c.id, NOW(), 'inherit' FROM channels c
		WHERE c.type <> 'dm' AND ($2::uuid[] IS NULL OR c.id = ANY($2::uuid[]))
		  AND ($3::uuid IS NULL OR c.category_id = $3::uuid)
		ON CONFLICT (user_id, channel_id) DO UPDATE
		SET last_read_at = GREATEST(COALESCE(channel_reads.last_read_at, EXCLUDED.last_read_at), EXCLUDED.last_read_at)`,
		userID, visible, categoryID)
	if err != nil {
		return fmt.Errorf("mark all read: %w", err)
	}
	return nil
}
