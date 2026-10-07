//go:build integration

package db

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io/fs"
	"net/url"
	"os"
	"strings"
	"testing"
	"testing/fstest"
)

// scratchPool connects to TEST_DATABASE_URL with search_path set to a fresh
// schema (then public), so these tests never touch the shared tables other
// packages use.
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
	// public stays on the path for extensions (pg_trgm) another test or
	// the shared database already installed there.
	q.Set("search_path", schema+",public")
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

// Usernames differing only in case are rejected by the database (0011). Runs
// in a rolled-back transaction, so the shared test database stays untouched.
func TestUsernamesAreUniqueIgnoringCase(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	p, err := Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if err := p.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	tx, err := p.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	name := "CaseTest" + hex.EncodeToString(b)
	insert := `INSERT INTO users (username, display_name, password_hash) VALUES ($1, 'x', 'x')`
	if _, err := tx.Exec(ctx, insert, name); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, insert, strings.ToLower(name)); err == nil {
		t.Fatal("username differing only in case was accepted")
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

// 0013 numbers existing channels and messages in creation order (not in
// insertion order) and new rows continue after them.
func TestReadableNumbersBackfillInCreationOrder(t *testing.T) {
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
		if name >= "0013" {
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

	// The seed channels of 0001 are older than these; "late" is inserted
	// first but created after "early".
	var late, early, user string
	if err := p.QueryRow(ctx, `INSERT INTO channels (name, type, created_at) VALUES ('late', 'text', NOW() + INTERVAL '2 hours') RETURNING id`).Scan(&late); err != nil {
		t.Fatal(err)
	}
	if err := p.QueryRow(ctx, `INSERT INTO channels (name, type, created_at) VALUES ('early', 'text', NOW() + INTERVAL '1 hour') RETURNING id`).Scan(&early); err != nil {
		t.Fatal(err)
	}
	if err := p.QueryRow(ctx, `INSERT INTO users (username, display_name, password_hash) VALUES ('numbers', 'n', 'x') RETURNING id`).Scan(&user); err != nil {
		t.Fatal(err)
	}
	var second, first string
	if err := p.QueryRow(ctx, `INSERT INTO messages (channel_id, user_id, content, created_at) VALUES ($1, $2, 'second', NOW() + INTERVAL '1 minute') RETURNING id`, early, user).Scan(&second); err != nil {
		t.Fatal(err)
	}
	if err := p.QueryRow(ctx, `INSERT INTO messages (channel_id, user_id, content, created_at) VALUES ($1, $2, 'first', NOW()) RETURNING id`, early, user).Scan(&first); err != nil {
		t.Fatal(err)
	}

	if err := p.MigrateFS(ctx, sub); err != nil {
		t.Fatal(err)
	}
	number := func(table, id string) int64 {
		var n int64
		if err := p.QueryRow(ctx, `SELECT number FROM `+table+` WHERE id = $1`, id).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	var channels int64
	if err := p.QueryRow(ctx, `SELECT COUNT(*) FROM channels`).Scan(&channels); err != nil {
		t.Fatal(err)
	}
	if number("channels", early) != channels-1 || number("channels", late) != channels {
		t.Errorf("channel numbers early=%d late=%d of %d, want creation order at the end", number("channels", early), number("channels", late), channels)
	}
	if number("messages", first) != 1 || number("messages", second) != 2 {
		t.Errorf("message numbers first=%d second=%d, want 1 and 2", number("messages", first), number("messages", second))
	}

	var next string
	if err := p.QueryRow(ctx, `INSERT INTO messages (channel_id, user_id, content) VALUES ($1, $2, 'third') RETURNING id`, early, user).Scan(&next); err != nil {
		t.Fatal(err)
	}
	if n := number("messages", next); n != 3 {
		t.Errorf("new message number %d, want 3", n)
	}
	if _, err := p.Exec(ctx, `INSERT INTO messages (channel_id, user_id, content, number) VALUES ($1, $2, 'forged', 99)`, early, user); err == nil {
		t.Error("an explicit number was accepted")
	}
}
