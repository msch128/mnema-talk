//go:build integration

package db

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func corePoolWithConfig(t *testing.T, original *Pool, configure func(*pgxpool.Config)) *Pool {
	t.Helper()
	config := original.Config()
	config.MaxConns, config.MinConns, config.MinIdleConns = 1, 0, 0
	configure(config)
	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return &Pool{pool}
}

// Use pgx's supported tracing hooks to place a real request cancellation at
// a database operation boundary, without replacing the pool or its methods.
type migrationCancellationTracer struct {
	query           string
	afterPrepare    bool
	preparingTarget bool
	cancel          context.CancelFunc
}

func (tracer *migrationCancellationTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.Contains(data.SQL, tracer.query) {
		ctx, tracer.cancel = context.WithCancel(ctx)
		if !tracer.afterPrepare {
			tracer.cancel()
		}
	}
	return ctx
}

func (*migrationCancellationTracer) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {
}

func (tracer *migrationCancellationTracer) TracePrepareStart(ctx context.Context, _ *pgx.Conn, data pgx.TracePrepareStartData) context.Context {
	tracer.preparingTarget = strings.Contains(data.SQL, tracer.query)
	return ctx
}

func (tracer *migrationCancellationTracer) TracePrepareEnd(_ context.Context, _ *pgx.Conn, _ pgx.TracePrepareEndData) {
	if tracer.afterPrepare && tracer.cancel != nil && tracer.preparingTarget {
		tracer.cancel()
	}
}

func TestMigrateReportsClosedPoolAndCanceledDatabaseOperations(t *testing.T) {
	t.Run("closed pool", func(t *testing.T) {
		p := scratchPool(t)
		p.Close()
		if err := p.MigrateFS(context.Background(), fstest.MapFS{}); err == nil || !strings.Contains(err.Error(), "acquire connection") {
			t.Fatalf("closed pool migration: %v", err)
		}
	})
	for name, target := range map[string]string{
		"migration lock": "SELECT pg_advisory_lock",
		"add checksum":   "ALTER TABLE schema_migrations",
	} {
		t.Run(name, func(t *testing.T) {
			original := scratchPool(t)
			tracer := &migrationCancellationTracer{query: target}
			p := corePoolWithConfig(t, original, func(config *pgxpool.Config) {
				config.ConnConfig.Tracer = tracer
			})
			err := p.MigrateFS(context.Background(), fstest.MapFS{})
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("canceled %s migration: %v", name, err)
			}
		})
	}
}

func TestMigrateRejectsReadOnlyDatabaseWithoutApplyingFiles(t *testing.T) {
	original := scratchPool(t)
	p := corePoolWithConfig(t, original, func(config *pgxpool.Config) {
		config.ConnConfig.RuntimeParams["default_transaction_read_only"] = "on"
	})
	err := p.MigrateFS(context.Background(), fstest.MapFS{"0001_new.sql": {Data: []byte("CREATE TABLE should_not_exist (id INTEGER)")}})
	var pgerr *pgconn.PgError
	if !errors.As(err, &pgerr) || pgerr.Code != "25006" || !strings.Contains(err.Error(), "create schema_migrations") {
		t.Fatalf("read-only migration error: %v", err)
	}
	var exists bool
	if err := original.QueryRow(context.Background(), `SELECT to_regclass('should_not_exist') IS NOT NULL`).Scan(&exists); err != nil || exists {
		t.Fatalf("read-only migration changed schema: exists=%v err=%v", exists, err)
	}
}

func TestMigrateRejectsMalformedMetadataAndCanceledRowStream(t *testing.T) {
	for name, ddl := range map[string]string{
		"missing filename": `CREATE TABLE schema_migrations (other TEXT, checksum TEXT)`,
		"null filename":    `CREATE TABLE schema_migrations (filename TEXT, checksum TEXT); INSERT INTO schema_migrations VALUES (NULL, 'sum')`,
	} {
		t.Run(name, func(t *testing.T) {
			p := scratchPool(t)
			if _, err := p.Exec(context.Background(), ddl); err != nil {
				t.Fatal(err)
			}
			err := p.MigrateFS(context.Background(), fstest.MapFS{})
			if err == nil {
				t.Fatal("malformed migration metadata accepted")
			}
			if name == "missing filename" && !strings.Contains(err.Error(), "read schema_migrations") {
				t.Fatalf("missing filename error: %v", err)
			}
			if name == "null filename" && !strings.Contains(err.Error(), "NULL") {
				t.Fatalf("null filename scan error: %v", err)
			}
		})
	}
	t.Run("canceled after prepare before row stream", func(t *testing.T) {
		original := scratchPool(t)
		tracer := &migrationCancellationTracer{query: "SELECT filename, COALESCE", afterPrepare: true}
		p := corePoolWithConfig(t, original, func(config *pgxpool.Config) {
			config.ConnConfig.Tracer = tracer
		})
		if err := p.MigrateFS(context.Background(), fstest.MapFS{}); !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled migration row stream: %v", err)
		}
	})
}

type unreadableMigrationFile struct{ fstest.MapFS }

func (unreadableMigrationFile) ReadFile(name string) ([]byte, error) {
	return nil, &fs.PathError{Op: "read", Path: name, Err: fs.ErrPermission}
}

func TestMigratePropagatesMigrationFilesystemFailures(t *testing.T) {
	for name, filesystem := range map[string]fs.FS{
		"directory": unreadableMigrationDirectory{},
		"file":      unreadableMigrationFile{fstest.MapFS{"0001_missing.sql": {Data: []byte("SELECT 1")}}},
	} {
		t.Run(name, func(t *testing.T) {
			p := scratchPool(t)
			if err := p.MigrateFS(context.Background(), filesystem); !errors.Is(err, fs.ErrPermission) {
				t.Fatalf("unreadable migration %s: %v", name, err)
			}
		})
	}
}

func TestMigrateBackfillFailureAndApplicationFailurePreserveSchema(t *testing.T) {
	t.Run("backfill constraint", func(t *testing.T) {
		p := scratchPool(t)
		if _, err := p.Exec(context.Background(), `CREATE TABLE schema_migrations (filename TEXT PRIMARY KEY, checksum TEXT CHECK (checksum IS NULL)); INSERT INTO schema_migrations VALUES ('0001_existing.sql', NULL)`); err != nil {
			t.Fatal(err)
		}
		filesystem := fstest.MapFS{"0001_existing.sql": {Data: []byte("CREATE TABLE should_not_exist (id INTEGER)")}}
		err := p.MigrateFS(context.Background(), filesystem)
		if err == nil || !strings.Contains(err.Error(), "record checksum of 0001_existing.sql") {
			t.Fatalf("backfill failure: %v", err)
		}
		var missing bool
		if err := p.QueryRow(context.Background(), `SELECT checksum IS NULL FROM schema_migrations`).Scan(&missing); err != nil || !missing {
			t.Fatalf("failed backfill changed checksum: missing=%v err=%v", missing, err)
		}
	})
	for name, values := range map[string][2]string{
		"SQL failure":    {``, `CREATE TABLE rolled_back (id INTEGER); SELECT 1/0`},
		"record failure": {`CREATE TABLE schema_migrations (filename TEXT PRIMARY KEY CHECK (filename <> '0001_failed.sql'), checksum TEXT)`, `CREATE TABLE rolled_back (id INTEGER)`},
	} {
		t.Run(name, func(t *testing.T) {
			ddl, migration := values[0], values[1]
			p := scratchPool(t)
			if ddl != "" {
				if _, err := p.Exec(context.Background(), ddl); err != nil {
					t.Fatal(err)
				}
			}
			err := p.MigrateFS(context.Background(), fstest.MapFS{"0001_failed.sql": {Data: []byte(migration)}})
			if err == nil || !strings.Contains(err.Error(), "apply migration 0001_failed.sql") {
				t.Fatalf("failed migration: %v", err)
			}
			var exists bool
			if err := p.QueryRow(context.Background(), `SELECT to_regclass('rolled_back') IS NOT NULL`).Scan(&exists); err != nil || exists {
				t.Fatalf("failed transaction left DDL: exists=%v err=%v", exists, err)
			}
			var applied int
			if err := p.QueryRow(context.Background(), `SELECT count(*) FROM schema_migrations`).Scan(&applied); err != nil || applied != 0 {
				t.Fatalf("failed transaction recorded migration: count=%d err=%v", applied, err)
			}
		})
	}
}
