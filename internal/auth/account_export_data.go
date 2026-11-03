package auth

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/userdata"
)

// Settings and the audit entries of an export. Channel names appear only for
// channels the member can still view. An audit entry says "you" or "staff".

func exportSettings(ctx context.Context, q db.Querier, user uuid.UUID, viewable userdata.Channels) (capped, error) {
	type channelSetting struct {
		ChannelID   uuid.UUID  `json:"channel_id" db:"channel_id"`
		ChannelName *string    `json:"channel_name" db:"name"`
		Level       string     `json:"notify_level" db:"notify_level"`
		MutedUntil  *time.Time `json:"muted_until" db:"muted_until"`
		LastReadAt  *time.Time `json:"last_read_at" db:"last_read_at"`
	}
	channels, channelsCut, err := fetchRows[channelSetting](ctx, q, exportMaxRows, `SELECT cr.channel_id, c.name, cr.notify_level, cr.muted_until, cr.last_read_at
		FROM channel_reads cr JOIN channels c ON c.id = cr.channel_id WHERE cr.user_id = $1 ORDER BY c.name LIMIT $2`, user)
	if err != nil {
		return capped{}, err
	}
	for i := range channels {
		if !viewable.Has(channels[i].ChannelID) {
			channels[i].ChannelName = nil
		}
	}
	type categorySetting struct {
		CategoryID uuid.UUID  `json:"category_id" db:"category_id"`
		Level      string     `json:"notify_level" db:"notify_level"`
		MutedUntil *time.Time `json:"muted_until" db:"muted_until"`
	}
	categories, categoriesCut, err := fetchRows[categorySetting](ctx, q, exportMaxRows, `SELECT category_id, notify_level, muted_until
		FROM category_notify WHERE user_id = $1 LIMIT $2`, user)
	if err != nil {
		return capped{}, err
	}
	server, err := exportServerSetting(ctx, q, user)
	if err != nil {
		return capped{}, err
	}
	return capped{
		value:     map[string]any{"channels": channels, "categories": categories, "server": server},
		truncated: channelsCut || categoriesCut,
	}, nil
}

func exportServerSetting(ctx context.Context, q db.Querier, user uuid.UUID) (map[string]any, error) {
	rows, err := q.Query(ctx, `SELECT notify_level, muted_until, suppress_everyone FROM server_notify WHERE user_id = $1`, user)
	if err != nil {
		return nil, err
	}
	type serverSetting struct {
		Level            string     `json:"notify_level" db:"notify_level"`
		MutedUntil       *time.Time `json:"muted_until" db:"muted_until"`
		SuppressEveryone bool       `json:"suppress_everyone" db:"suppress_everyone"`
	}
	set, err := pgx.CollectOneRow(rows, pgx.RowToStructByName[serverSetting])
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return map[string]any{"notify_level": set.Level, "muted_until": set.MutedUntil, "suppress_everyone": set.SuppressEveryone}, nil
}

func exportAudit(ctx context.Context, q db.Querier, user uuid.UUID) (capped, error) {
	type entry struct {
		At     time.Time `json:"at" db:"created_at"`
		Action string    `json:"action" db:"action"`
		By     string    `json:"by" db:"by"`
		About  string    `json:"about" db:"about"`
	}
	return listDataset[entry](ctx, q, user, `SELECT created_at, action,
		CASE WHEN actor_id = $1 THEN 'you' ELSE 'staff' END AS by,
		CASE WHEN target_type = 'user' AND target_id = $1::text THEN 'your account' ELSE target_type END AS about
		FROM audit_log WHERE actor_id = $1 OR (target_type = 'user' AND target_id = $1::text) ORDER BY id LIMIT $2`)
}
