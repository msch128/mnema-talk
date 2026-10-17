//go:build integration

// Package testutil provides the shared Postgres fixture for integration tests
// (build tag "integration"), modelled on mnema.xyz's internal/testutil.
//
// A single postgres:18-alpine container is started per test binary (see
// package pgtest) and migrated with the embedded migrations. Set
// TEST_DATABASE_URL to use an existing database instead (CI service
// container). Tests isolate themselves with Reset, which truncates all data.
// Packages using DB call Main from TestMain so the container is removed.
package testutil

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/testutil/pgtest"
)

var (
	startOnce sync.Once
	shared    *db.Pool
	startErr  error
)

// DB returns the shared, migrated pool, starting Postgres on first use.
func DB(t testing.TB) *db.Pool {
	t.Helper()
	startOnce.Do(func() { shared, startErr = start() })
	if startErr != nil {
		t.Fatalf("start postgres: %v", startErr)
	}
	return shared
}

func start() (*db.Pool, error) {
	dsn, err := pgtest.DSN()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	p, err := connectAndMigrate(ctx, dsn)
	if err != nil {
		pgtest.Stop()
		return nil, err
	}
	return p, nil
}

// Main runs the package's tests and then removes the Postgres container (if
// one was started). Call it from TestMain: os.Exit(testutil.Main(m)).
// Leftovers from a killed test binary carry the label mnema-talk.testutil.
func Main(m *testing.M) int {
	code := m.Run()
	if shared != nil {
		shared.Close()
	}
	pgtest.Stop()
	return code
}

func connectAndMigrate(ctx context.Context, dsn string) (*db.Pool, error) {
	p, err := db.Connect(ctx, dsn)
	if err != nil {
		return nil, err
	}
	if err := p.Migrate(ctx); err != nil {
		p.Close()
		return nil, err
	}
	return p, nil
}

// Reset empties every application table (schema and migrations stay).
func Reset(t testing.TB, p *db.Pool) {
	t.Helper()
	_, err := p.Exec(context.Background(), `
		TRUNCATE message_reactions, media, messages, channels, categories, invites, users
		RESTART IDENTITY CASCADE`)
	if err != nil {
		t.Fatalf("reset database: %v", err)
	}
	// Roles are not user data: @everyone stays (back at its defaults), the
	// custom roles of the previous test go.
	_, err = p.Exec(context.Background(), `DELETE FROM roles WHERE NOT is_everyone`)
	if err == nil {
		_, err = p.Exec(context.Background(), `UPDATE roles SET permissions = $1 WHERE is_everyone`, int64(perm.DefaultEveryone))
	}
	if err != nil {
		t.Fatalf("reset roles: %v", err)
	}
}
