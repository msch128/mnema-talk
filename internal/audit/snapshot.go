package audit

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/msch128/mnema-talk/internal/db"
)

// snapshotSQL reads the audited columns of a resource as JSON, plus a display
// name. Columns are listed one by one: a secret column added to a table later
// must not leak into the log by accident. Every query takes the id as $1.
var snapshotSQL = map[Resource]string{
	ResRole: `SELECT jsonb_build_object('name', name, 'color', color, 'position', position,
		'permissions', permissions, 'hoist', hoist, 'mentionable', mentionable), name
		FROM roles WHERE id = $1`,
	ResChannel: `SELECT jsonb_build_object('name', name, 'type', type, 'topic', topic,
		'category_id', category_id, 'user_limit', user_limit, 'sort_order', sort_order), name
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
		'role_ids', COALESCE((SELECT jsonb_agg(role_id ORDER BY role_id) FROM user_roles WHERE user_id = users.id), '[]'::jsonb)),
		username FROM users WHERE id = $1`,
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
	uid, err := uuid.Parse(id)
	if !ok || err != nil {
		return nil, ""
	}
	var raw []byte
	var name string
	if err := q.QueryRow(ctx, query, uid).Scan(&raw, &name); err != nil {
		return nil, ""
	}
	// The queries above build JSON objects, so this cannot fail.
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return out, name
}

// diff lists the fields of the request that differ from the snapshot.
func diff(before, req map[string]any) map[string]any {
	changes := map[string]any{}
	for k, to := range req {
		from, ok := before[k]
		if !ok {
			continue
		}
		if fmt.Sprint(from) == fmt.Sprint(to) {
			continue
		}
		changes[k] = map[string]any{"from": from, "to": to}
	}
	return changes
}
