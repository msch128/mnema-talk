//go:build integration

package db

import (
	"context"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
)

// trigramIndexes lists the GIN trigram indexes on messages.content in the
// test schema.
func trigramIndexes(t *testing.T, p *Pool) []string {
	t.Helper()
	rows, err := p.Query(context.Background(), `
		SELECT indexname FROM pg_indexes
		WHERE schemaname = current_schema() AND tablename = 'messages'
		  AND indexdef ILIKE '%gin%' AND indexdef ILIKE '%gin_trgm_ops%'
		ORDER BY indexname`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	return names
}

func TestMigrationsLeaveExactlyOneTrigramIndexAndSearchUsesIt(t *testing.T) {
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
	// An instance on the previous release carries both copies of the index.
	const dropMigration = "0038_drop_duplicate_trigram.sql"
	before := fstest.MapFS{}
	for _, name := range files {
		if name >= dropMigration {
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
	if got := trigramIndexes(t, p); len(got) != 2 {
		t.Fatalf("before the upgrade: %v, want both duplicates", got)
	}

	if err := p.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	got := trigramIndexes(t, p)
	if len(got) != 1 || got[0] != "messages_content_trgm_idx" {
		t.Fatalf("after the upgrade: %v, want only messages_content_trgm_idx", got)
	}

	var plan strings.Builder
	tx, err := p.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// A near-empty table would be scanned sequentially whatever the indexes are.
	if _, err := tx.Exec(ctx, `SET LOCAL enable_seqscan = off`); err != nil {
		t.Fatal(err)
	}
	rows, err := tx.Query(ctx, `EXPLAIN SELECT id FROM messages WHERE content ILIKE '%needle%'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		plan.WriteString(line + "\n")
	}
	if !strings.Contains(plan.String(), "messages_content_trgm_idx") {
		t.Fatalf("substring search does not use the trigram index:\n%s", plan.String())
	}
}
