// Package db owns the PostgreSQL connection pool and the embedded schema
// migrations. The administrator is seeded by auth.EnsureAdminUser.
package db

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// migrationLockID serialises concurrent migrators (pg_advisory_lock key).
const migrationLockID = 0x6d6e656d61 // "mnema"

type Pool struct {
	*pgxpool.Pool
}

// Querier is satisfied by both *Pool and pgx.Tx, so data functions can run
// inside or outside a transaction.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

func Connect(ctx context.Context, databaseURL string) (*Pool, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database config: %w", err)
	}

	config.MaxConns = 25
	config.MinConns = 2
	config.MaxConnLifetime = time.Hour
	config.MaxConnIdleTime = 15 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("connect to database: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return &Pool{pool}, nil
}

// Migrate applies every embedded migration that has not run yet, in filename
// order, each in its own transaction. It is forward-only and safe to call from
// several instances at once thanks to an advisory lock.
//
// Each applied migration's SHA-256 is recorded; startup fails when an already
// applied file was changed afterwards (migrations must never be edited).
func (p *Pool) Migrate(ctx context.Context) error {
	sub, err := fs.Sub(migrationFS, "migrations")
	if err != nil {
		return err
	}
	return p.MigrateFS(ctx, sub)
}

// MigrateFS applies the *.sql files at the root of fsys like Migrate does.
// Migrate uses the embedded migrations; tests pass their own.
func (p *Pool) MigrateFS(ctx context.Context, fsys fs.FS) error {
	conn, err := p.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire connection: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, migrationLockID); err != nil {
		return fmt.Errorf("acquire migration lock: %w", err)
	}
	defer func() {
		_, _ = conn.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, migrationLockID)
	}()

	if _, err := conn.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			filename   TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	// Added after the first releases; rows from before stay NULL until they
	// are backfilled below.
	if _, err := conn.Exec(ctx, `ALTER TABLE schema_migrations ADD COLUMN IF NOT EXISTS checksum TEXT`); err != nil {
		return fmt.Errorf("add schema_migrations.checksum: %w", err)
	}

	applied := map[string]string{} // filename -> checksum ("" = not recorded yet)
	rows, err := conn.Query(ctx, `SELECT filename, COALESCE(checksum, '') FROM schema_migrations`)
	if err != nil {
		return fmt.Errorf("read schema_migrations: %w", err)
	}
	for rows.Next() {
		var name, sum string
		if err := rows.Scan(&name, &sum); err != nil {
			rows.Close()
			return err
		}
		applied[name] = sum
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	files, err := listMigrations(fsys)
	if err != nil {
		return err
	}
	contents := make(map[string][]byte, len(files))
	sums := make(map[string]string, len(files))
	for _, name := range files {
		b, err := fs.ReadFile(fsys, name)
		if err != nil {
			return err
		}
		contents[name], sums[name] = b, Checksum(b)
	}

	backfill, err := verifyChecksums(applied, sums)
	if err != nil {
		return err
	}
	for _, name := range backfill {
		if _, err := conn.Exec(ctx, `UPDATE schema_migrations SET checksum = $2 WHERE filename = $1 AND checksum IS NULL`, name, sums[name]); err != nil {
			return fmt.Errorf("record checksum of %s: %w", name, err)
		}
	}
	if len(backfill) > 0 {
		slog.Info("migration checksums recorded", "count", len(backfill))
	}

	for _, name := range files {
		if _, ok := applied[name]; ok {
			continue
		}
		err = pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, string(contents[name])); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `INSERT INTO schema_migrations (filename, checksum) VALUES ($1, $2)`, name, sums[name])
			return err
		})
		if err != nil {
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
		slog.Info("migration applied", "file", name)
	}
	return nil
}

// Checksum is the hex SHA-256 of a migration file, as stored in
// schema_migrations.checksum. CRLF line endings are hashed as LF, so a binary
// built from a Windows checkout (git autocrlf) matches one built on Linux.
func Checksum(b []byte) string {
	h := sha256.Sum256(bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n")))
	return hex.EncodeToString(h[:])
}

// verifyChecksums compares the recorded checksums of applied migrations with
// the shipped files. It fails when an applied file changed and returns the
// applied files whose checksum was never recorded (to backfill). Applied
// migrations that no longer ship are ignored, unless they sort after the
// newest shipped one: then a newer version already ran against this
// database, and this older one must not start on a schema it doesn't know.
func verifyChecksums(applied, files map[string]string) ([]string, error) {
	newest := ""
	for name := range files {
		newest = max(newest, name)
	}
	var backfill, ahead []string
	for name, recorded := range applied {
		sum, ok := files[name]
		if !ok {
			if name > newest {
				ahead = append(ahead, name)
			}
			continue
		}
		if recorded == "" {
			backfill = append(backfill, name)
			continue
		}
		if recorded != sum {
			return nil, fmt.Errorf("migration %s was changed after it was applied (checksum %s, recorded %s): never edit an applied migration, add a new one", name, sum, recorded)
		}
	}
	if len(ahead) > 0 {
		sort.Strings(ahead)
		return nil, fmt.Errorf("the database was migrated by a newer version (%s, this version ships up to %s): run that version or newer, or restore the backup taken before the update (doc/upgrade.md, Rollback)", strings.Join(ahead, ", "), newest)
	}
	sort.Strings(backfill)
	return backfill, nil
}

// MigrationFiles lists the embedded migrations in apply order.
func MigrationFiles() ([]string, error) {
	sub, err := fs.Sub(migrationFS, "migrations")
	if err != nil {
		return nil, err
	}
	return listMigrations(sub)
}

func listMigrations(fsys fs.FS) ([]string, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}
