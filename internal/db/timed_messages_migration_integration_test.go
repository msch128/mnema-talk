//go:build integration

package db

import (
	"context"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/google/uuid"
)

// 0105 on a database with data: channels of every kind (a text channel with a
// period, one turned off by its own setting, one never set, a conversation
// and a call Talk that still holds a period from before the routes refused
// it), a message, a file, a scheduled message and the settings row.
func TestTimedMessagesMigrationKeepsEveryRow(t *testing.T) {
	p := scratchPool(t)
	ctx := context.Background()
	sub, err := fs.Sub(migrationFS, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	files, err := listMigrations(sub)
	if err != nil {
		t.Fatal(err)
	}
	before := fstest.MapFS{}
	for _, name := range files {
		if name >= "0105" {
			break
		}
		b, err := fs.ReadFile(sub, name)
		if err != nil {
			t.Fatal(err)
		}
		before[name] = &fstest.MapFile{Data: b}
	}
	if err := p.MigrateFS(ctx, before); err != nil {
		t.Fatal(err)
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := p.Exec(ctx, q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	user, week, off, never, dm, call := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	exec(`INSERT INTO users (id, username, display_name, password_hash) VALUES ($1, 'alice', 'Alice', 'x')`, user)
	exec(`INSERT INTO channels (id, name, type, auto_delete_days) VALUES ($1, 'week', 'text', 7), ($2, 'off', 'text', 0), ($3, 'never', 'text', 0)`, week, off, never)
	exec(`INSERT INTO channels (id, number, name, type, dm_key) OVERRIDING SYSTEM VALUE VALUES ($1, 99999, '', 'dm', 'a:b')`, dm)
	exec(`INSERT INTO channels (id, name, type, dm_id, is_temp, auto_delete_days) VALUES ($1, 'call', 'voice', $2, TRUE, 1)`, call, dm)
	// The "off" channel was turned on and then off again; "never" was never set.
	exec(`INSERT INTO audit_log (action, target_type, target_id, details) VALUES
		('channel.auto_delete_set', 'channel', $1, '{"after": {"auto_delete_days": 30}}'),
		('channel.auto_delete_set', 'channel', $1, '{"after": {"auto_delete_days": 0}}'),
		('channel.auto_delete_set', 'channel', $2, '{"after": {"auto_delete_days": 7}}')`, off.String(), week.String())
	var msg uuid.UUID
	if err := p.QueryRow(ctx, `INSERT INTO messages (channel_id, user_id, content) VALUES ($1, $2, 'kept') RETURNING id`, week, user).Scan(&msg); err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO media (uploader_id, channel_id, message_id, s3_bucket, s3_key, original_filename, mime_type, size_bytes) VALUES ($1, $2, $3, 'b', 'k', 'a.png', 'image/png', 3)`, user, week, msg)
	exec(`INSERT INTO scheduled_messages (user_id, channel_id, content, send_at) VALUES ($1, $2, 'later', now() + interval '1 day')`, user, week)

	if err := p.MigrateFS(ctx, sub); err != nil {
		t.Fatalf("0105 on seeded data: %v", err)
	}
	for id, want := range map[uuid.UUID]*int{week: ptr(168), off: ptr(0), never: nil, dm: nil, call: nil} {
		var got *int
		if err := p.QueryRow(ctx, `SELECT auto_delete_hours FROM channels WHERE id = $1`, id).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if (got == nil) != (want == nil) || got != nil && *got != *want {
			t.Errorf("channel %s: auto_delete_hours %v, want %v", id, deref(got), deref(want))
		}
	}
	var expires, width, lifetime *int
	var enabled bool
	var maxSeconds, defaultHours int
	if err := p.QueryRow(ctx, `SELECT (SELECT EXTRACT(EPOCH FROM expires_at)::int FROM messages WHERE id = $1),
		(SELECT width FROM media WHERE message_id = $1), (SELECT lifetime_seconds FROM scheduled_messages),
		message_lifetimes_enabled, message_lifetime_max_seconds, auto_delete_default_hours FROM instance_settings`, msg).
		Scan(&expires, &width, &lifetime, &enabled, &maxSeconds, &defaultHours); err != nil {
		t.Fatal(err)
	}
	if expires != nil || width != nil || lifetime != nil || !enabled || maxSeconds != 2592000 || defaultHours != 0 {
		t.Errorf("after 0105: expires %v, width %v, lifetime %v, enabled %v, max %d, default %d", expires, width, lifetime, enabled, maxSeconds, defaultHours)
	}
	if _, err := p.Exec(ctx, `UPDATE channels SET auto_delete_hours = 24 WHERE id = $1`, call); err == nil {
		t.Error("the schema took a period on a call Talk")
	}
}

func ptr(n int) *int { return &n }

func deref(p *int) any {
	if p == nil {
		return "NULL"
	}
	return *p
}
