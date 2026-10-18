//go:build integration

package db

import (
	"context"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
)

// 0033 gives every existing bot a role named after it; reserved role names
// (@everyone, @here) get the " bot" suffix the runtime adds (bot.roleName),
// within the 32 characters a role name has.
func TestBotOwnRoleBackfillMapsReservedNames(t *testing.T) {
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
		if name >= "0033" {
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
	long := strings.Repeat("x", 32)
	want := map[string]string{
		"everyone": "everyone bot", "@Here": "@Here bot", " HERE": " HERE bot", "plain": "plain", long: long,
		"@everyone" + strings.Repeat("y", 23): "@everyone" + strings.Repeat("y", 23),
	}
	i := 0
	for name := range want {
		i++
		var user string
		if err := p.QueryRow(ctx, `INSERT INTO users (username, display_name, password_hash, is_bot) VALUES ($1, 'b', 'x', TRUE) RETURNING id`, "bot"+string(rune('a'+i))).Scan(&user); err != nil {
			t.Fatal(err)
		}
		if _, err := p.Exec(ctx, `INSERT INTO bots (user_id, name) VALUES ($1, $2)`, user, name); err != nil {
			t.Fatal(err)
		}
	}
	if err := p.MigrateFS(ctx, sub); err != nil {
		t.Fatal(err)
	}
	for name, role := range want {
		var got string
		if err := p.QueryRow(ctx, `SELECT r.name FROM roles r JOIN bots b ON b.user_id = r.bot_user_id WHERE b.name = $1`, name).Scan(&got); err != nil {
			t.Fatalf("bot %q: %v", name, err)
		}
		if got != role {
			t.Errorf("bot %q got role %q, want %q", name, got, role)
		}
	}
}
