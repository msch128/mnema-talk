//go:build integration

package db

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"strings"
	"testing"
	"testing/fstest"
)

// scratchPool connects to TEST_DATABASE_URL with search_path set to a fresh
// schema, so these tests never touch the shared tables other packages use.
func scratchPool(t *testing.T) *Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	schema := "migtest_" + hex.EncodeToString(b)

	admin, err := Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), `DROP SCHEMA `+schema+` CASCADE`)
		admin.Close()
	})

	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	p, err := Connect(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	return p
}

func TestMigrateRecordsAndVerifiesChecksums(t *testing.T) {
	p := scratchPool(t)
	ctx := context.Background()
	fsys := fstest.MapFS{
		"0001_a.sql": {Data: []byte(`CREATE TABLE a (x INT);`)},
		"0002_b.sql": {Data: []byte(`CREATE TABLE b (x INT);`)},
	}
	if err := p.MigrateFS(ctx, fsys); err != nil {
		t.Fatal(err)
	}
	var sum string
	if err := p.QueryRow(ctx, `SELECT checksum FROM schema_migrations WHERE filename = '0001_a.sql'`).Scan(&sum); err != nil {
		t.Fatal(err)
	}
	if sum != Checksum(fsys["0001_a.sql"].Data) {
		t.Fatalf("checksum = %q", sum)
	}

	// Rows from before the checksum column existed are backfilled.
	if _, err := p.Exec(ctx, `UPDATE schema_migrations SET checksum = NULL`); err != nil {
		t.Fatal(err)
	}
	if err := p.MigrateFS(ctx, fsys); err != nil {
		t.Fatalf("backfill run: %v", err)
	}
	var missing int
	if err := p.QueryRow(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE checksum IS NULL`).Scan(&missing); err != nil || missing != 0 {
		t.Fatalf("checksums not backfilled: missing=%d err=%v", missing, err)
	}

	// Editing an applied migration stops startup.
	fsys["0001_a.sql"] = &fstest.MapFile{Data: []byte(`CREATE TABLE a (x INT, y INT);`)}
	err := p.MigrateFS(ctx, fsys)
	if err == nil || !strings.Contains(err.Error(), "0001_a.sql") {
		t.Fatalf("edited migration accepted: %v", err)
	}
}

// A pre-checksum schema_migrations table (from older releases) gets the
// column added and its rows backfilled.
func TestMigrateUpgradesOldSchemaMigrationsTable(t *testing.T) {
	p := scratchPool(t)
	ctx := context.Background()
	if _, err := p.Exec(ctx, `
		CREATE TABLE schema_migrations (filename TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW());
		CREATE TABLE a (x INT);
		INSERT INTO schema_migrations (filename) VALUES ('0001_a.sql');`); err != nil {
		t.Fatal(err)
	}
	fsys := fstest.MapFS{"0001_a.sql": {Data: []byte(`CREATE TABLE a (x INT);`)}}
	if err := p.MigrateFS(ctx, fsys); err != nil {
		t.Fatal(err)
	}
	var sum string
	if err := p.QueryRow(ctx, `SELECT checksum FROM schema_migrations WHERE filename = '0001_a.sql'`).Scan(&sum); err != nil || sum == "" {
		t.Fatalf("checksum=%q err=%v", sum, err)
	}
}
