//go:build integration

package db

import (
	"context"
	"fmt"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

// 0075 gives every channel but a direct message a slug, in the order
// (created_at, id), with the same rules chat.Slugify applies at runtime.
func TestChannelSlugBackfill(t *testing.T) {
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
		if name >= "0075" {
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
	if _, err := p.Exec(ctx, `DELETE FROM channels`); err != nil {
		t.Fatal(err)
	}
	long := strings.Repeat("x", 64)
	names := []string{"General", "general", "Gr\u00f6\u00dfe & \u00c4rger", "Café", "🎮", "general!", long, long, "123e4567-e89b-12d3-a456-426614174000"}
	// The collision suffix is the first four hex characters of the channel's own id.
	want := []string{"general", "general-0002", "groesse-aerger", "cafe", "channel", "general-0006", long[:60], long[:55] + "-0008", "channel-123e4567-e89b-12d3-a456-426614174000"}
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i, name := range names {
		if _, err := p.Exec(ctx, `INSERT INTO channels (id, name, type, created_at) VALUES ($1, $2, 'text', $3)`, fmt.Sprintf("%04x0000-0000-4000-8000-000000000000", i+1), name, start.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := p.Exec(ctx, `INSERT INTO channels (id, name, type, is_temp, created_at) VALUES ('abcdef12-0000-4000-8000-000000000000', 'Talk 1', 'voice', TRUE, $1)`, start); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Exec(ctx, `INSERT INTO channels (number, name, type, dm_key) OVERRIDING SYSTEM VALUE VALUES (9999, '', 'dm', 'a:b')`); err != nil {
		t.Fatal(err)
	}
	if err := p.MigrateFS(ctx, sub); err != nil {
		t.Fatal(err)
	}
	for i, name := range names {
		var got string
		var history int
		err := p.QueryRow(ctx, `
			SELECT c.slug, (SELECT count(*) FROM channel_slugs s WHERE s.channel_id = c.id AND s.slug = c.slug)
			FROM channels c WHERE c.name = $1 AND c.created_at = $2`, name, start.Add(time.Duration(i)*time.Minute)).Scan(&got, &history)
		if err != nil {
			t.Fatalf("channel %q: %v", name, err)
		}
		if got != want[i] || history != 1 {
			t.Errorf("channel %q: slug %q (history %d), want %q", name, got, history, want[i])
		}
	}
	var talk string
	var rows int
	if err := p.QueryRow(ctx, `SELECT slug, (SELECT count(*) FROM channel_slugs WHERE channel_id = c.id) FROM channels c WHERE is_temp`).Scan(&talk, &rows); err != nil || talk != "talk-abcdef12" || rows != 0 {
		t.Fatalf("temporary Talk: slug %q, history %d, err %v", talk, rows, err)
	}
	var dm *string
	if err := p.QueryRow(ctx, `SELECT slug FROM channels WHERE type = 'dm'`).Scan(&dm); err != nil || dm != nil {
		t.Fatalf("a conversation has a slug: %v, err %v", dm, err)
	}
}
