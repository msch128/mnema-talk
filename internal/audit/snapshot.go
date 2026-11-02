package audit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/msch128/mnema-talk/internal/db"
)

// snapshotSQL reads the audited columns of a resource as JSON, plus a display
// name. Columns are listed one by one: a secret column added to a table later
// must not leak into the log by accident. Every query takes the id as $1.
var snapshotSQL = map[Resource]string{
	ResRole: `SELECT jsonb_build_object('name', name, 'color', color, 'position', position,
		'permissions', permissions, 'hoist', hoist, 'mentionable', mentionable, 'max_upload_mb', max_upload_mb), name
		FROM roles WHERE id = $1`,
	ResChannel: `SELECT jsonb_build_object('name', name, 'type', type, 'topic', topic,
		'category_id', category_id, 'user_limit', user_limit, 'slow_mode_seconds', slow_mode_seconds, 'auto_delete_days', auto_delete_days, 'sort_order', sort_order, 'is_lobby', is_lobby, 'is_stage', is_stage), name
		FROM channels WHERE id = $1`,
	ResCategory: `SELECT jsonb_build_object('name', name, 'sort_order', sort_order), name
		FROM categories WHERE id = $1`,
	ResWebhook: `SELECT jsonb_build_object('name', name, 'channel_id', channel_id), name
		FROM webhooks WHERE id = $1`,
	ResOutgoing: `SELECT jsonb_build_object('name', name, 'url', regexp_replace(url, '[?#].*$', ''),
		'events', events, 'enabled', enabled, 'channel_id', channel_id), name
		FROM outgoing_webhooks WHERE id = $1`,
	ResUser: `SELECT jsonb_build_object('username', username, 'display_name', display_name,
		'disabled', disabled_at IS NOT NULL, 'status_text', status_text,
		'timeout_until', CASE WHEN timeout_until > NOW() THEN timeout_until END,
		'banned', banned_at IS NOT NULL, 'banned_until', banned_until,
		'role_ids', COALESCE((SELECT jsonb_agg(role_id ORDER BY role_id) FROM user_roles WHERE user_id = users.id), '[]'::jsonb)),
		username FROM users WHERE id = $1`,
	ResSettings: `SELECT jsonb_build_object('max_upload_mb', max_upload_mb), 'instance settings' FROM instance_settings`,
	ResOnboarding: `SELECT jsonb_build_object('community_name', community_name, 'welcome_text', welcome_text,
		'rules_text', rules_text, 'rules_version', rules_version, 'start_channel_id', start_channel_id,
		'community_icon', community_icon_version IS NOT NULL),
		'onboarding' FROM instance_settings`,
	ResVanity: `SELECT jsonb_build_object('slug', vanity_slug, 'accepts_signups', vanity_accepts_signups), 'vanity link' FROM instance_settings`,
	ResEmoji:  `SELECT jsonb_build_object('name', name, 'animated', animated), name FROM custom_emoji WHERE id = $1`,
	ResBot: `SELECT jsonb_build_object('name', b.name, 'allowed_intents', b.allowed_intents, 'disabled', b.disabled,
		'role_ids', COALESCE((SELECT jsonb_agg(role_id ORDER BY role_id) FROM user_roles WHERE user_id = b.user_id), '[]'::jsonb),
		'active_tokens', (SELECT COUNT(*) FROM bot_tokens t WHERE t.bot_id = b.id AND t.revoked_at IS NULL
		                  AND (t.expires_at IS NULL OR t.expires_at > NOW())),
		'commands', (SELECT COUNT(*) FROM bot_commands c WHERE c.bot_id = b.id)), b.name
		FROM bots b WHERE b.id = $1`,
	ResInvite: `SELECT jsonb_build_object('max_uses', max_uses, 'uses_count', uses_count, 'expires_at', expires_at), ''
		FROM invites WHERE id = $1`,
	ResChannelOvw: `SELECT jsonb_build_object('overwrites', COALESCE((SELECT jsonb_agg(jsonb_build_object(
			'type', CASE WHEN role_id IS NOT NULL THEN 'role' ELSE 'member' END,
			'id', COALESCE(role_id, user_id), 'allow', allow, 'deny', deny) ORDER BY id)
			FROM permission_overwrites WHERE channel_id = c.id), '[]'::jsonb)), c.name
		FROM channels c WHERE c.id = $1`,
	ResCatOvw: `SELECT jsonb_build_object('overwrites', COALESCE((SELECT jsonb_agg(jsonb_build_object(
			'type', CASE WHEN role_id IS NOT NULL THEN 'role' ELSE 'member' END,
			'id', COALESCE(role_id, user_id), 'allow', allow, 'deny', deny) ORDER BY id)
			FROM permission_overwrites WHERE category_id = c.id), '[]'::jsonb)), c.name
		FROM categories c WHERE c.id = $1`,
}

// Snapshot returns the audited fields of a resource and its display name; a
// missing resource yields nil.
func Snapshot(ctx context.Context, q db.Querier, res Resource, id string) (map[string]any, string) {
	query, ok := snapshotSQL[res]
	if !ok {
		return nil, ""
	}
	var args []any
	if res != ResSettings && res != ResOnboarding && res != ResVanity {
		uid, err := uuid.Parse(id)
		if err != nil {
			return nil, ""
		}
		args = append(args, uid)
	}
	var raw []byte
	var name string
	if err := q.QueryRow(ctx, query, args...).Scan(&raw, &name); err != nil {
		// A missing resource is expected (it was deleted); anything else
		// would silently strip the before/after block of every entry.
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.WarnContext(ctx, "audit snapshot failed", "resource", res, "err", err)
		}
		return nil, ""
	}
	// The queries above build JSON objects, so this cannot fail.
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return out, name
}

// diff lists the fields that differ between two snapshots, or between a
// snapshot and the request (keys match case-insensitively, as encoding/json
// does when the handler decodes them).
func diff(before, after map[string]any) map[string]any {
	changes := map[string]any{}
	lower := make(map[string]string, len(before))
	for k := range before {
		lower[strings.ToLower(k)] = k
	}
	for k, to := range after {
		bk, ok := lower[strings.ToLower(k)]
		if !ok {
			continue
		}
		from := before[bk]
		if fmt.Sprint(from) == fmt.Sprint(to) {
			continue
		}
		changes[bk] = map[string]any{"from": from, "to": to}
	}
	return changes
}
