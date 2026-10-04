// Package db owns the PostgreSQL connection pool, the embedded schema migrations
// and the first-start seed of the administrator account.
package db

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
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
func (p *Pool) Migrate(ctx context.Context) error {
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

	applied := map[string]bool{}
	rows, err := conn.Query(ctx, `SELECT filename FROM schema_migrations`)
	if err != nil {
		return fmt.Errorf("read schema_migrations: %w", err)
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return err
		}
		applied[name] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	files, err := MigrationFiles()
	if err != nil {
		return err
	}
	for _, name := range files {
		if applied[name] {
			continue
		}
		sql, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		err = pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, string(sql)); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `INSERT INTO schema_migrations (filename) VALUES ($1)`, name)
			return err
		})
		if err != nil {
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
		slog.Info("migration applied", "file", name)
	}
	return nil
}

// MigrationFiles lists the embedded migrations in apply order.
func MigrationFiles() ([]string, error) {
	entries, err := fs.ReadDir(migrationFS, "migrations")
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

// EnsureAdminUser seeds the administrator on first start. An empty password
// generates a random one, which is logged exactly once.
func (p *Pool) EnsureAdminUser(ctx context.Context, username, password string) error {
	var exists bool
	if err := p.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE role = 'admin')`).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return nil
	}

	generated := password == ""
	if generated {
		b := make([]byte, 18)
		if _, err := rand.Read(b); err != nil {
			return fmt.Errorf("generate admin password: %w", err)
		}
		password = base64.RawURLEncoding.EncodeToString(b)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash admin password: %w", err)
	}

	tag, err := p.Exec(ctx, `
		INSERT INTO users (username, display_name, password_hash, role)
		VALUES ($1, $1, $2, 'admin')
		ON CONFLICT (username) DO NOTHING
	`, username, string(hash))
	if err != nil {
		return fmt.Errorf("insert admin user: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return errors.New("admin username is taken by a regular user")
	}

	slog.Info("initial administrator created", "username", username)
	if generated {
		// Printed once on first start only; change it after the first login.
		slog.Warn("ADMIN_INITIAL_PASSWORD was empty, generated a password", "username", username, "password", password)
	}
	return nil
}
