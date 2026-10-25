//go:build integration

package db

import (
	"context"
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
	names := []string{"General", "general", "Größe & Ärger", "Café", "🎮", "general!", long, long, "123e4567-e89b-12d3-a456-426614174000"}
	want := []string{"general", "general-2", "groesse-aerger", "cafe", "channel", "general-3", long[:60], long[:58] + "-2", "channel-123e4567-e89b-12d3-a456-426614174000"}
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i, name := range names {
		if _, err := p.Exec(ctx, `INSERT INTO channels (name, type, created_at) VALUES ($1, 'text', $2)`, name, start.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatal(err)
		}
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
	var dm *string
	if err := p.QueryRow(ctx, `SELECT slug FROM channels WHERE type = 'dm'`).Scan(&dm); err != nil || dm != nil {
		t.Fatalf("a conversation has a slug: %v, err %v", dm, err)
	}
}
