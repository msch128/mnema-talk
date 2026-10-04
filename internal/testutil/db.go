//go:build integration

// Package testutil provides the shared Postgres fixture for integration tests
// (build tag "integration"), modelled on mnema.xyz's internal/testutil.
//
// A single postgres:17-alpine container is started per test binary via
// dockertest and migrated with the embedded migrations. Set
// TEST_DATABASE_URL to use an existing database instead (CI service
// container). Tests isolate themselves with Reset, which truncates all data.
package testutil

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/msch128/mnema-talk/internal/db"
	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
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
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	if dsn := os.Getenv("TEST_DATABASE_URL"); dsn != "" {
		return connectAndMigrate(ctx, dsn)
	}

	pool, err := dockertest.NewPool("")
	if err != nil {
		return nil, fmt.Errorf("dockertest pool: %w", err)
	}
	pool.MaxWait = 90 * time.Second

	res, err := pool.RunWithOptions(&dockertest.RunOptions{
		Repository: "postgres",
		Tag:        "17-alpine",
		Env:        []string{"POSTGRES_USER=mnema", "POSTGRES_PASSWORD=mnema", "POSTGRES_DB=mnema_test"},
	}, func(hc *docker.HostConfig) {
		hc.AutoRemove = true
		hc.RestartPolicy = docker.RestartPolicy{Name: "no"}
	})
	if err != nil {
		return nil, fmt.Errorf("run postgres: %w", err)
	}
	// Self-destruct even if the test binary dies before cleanup.
	_ = res.Expire(600)

	dsn := fmt.Sprintf("postgres://mnema:mnema@localhost:%s/mnema_test?sslmode=disable", res.GetPort("5432/tcp"))
	var p *db.Pool
	err = pool.Retry(func() error {
		var err error
		p, err = connectAndMigrate(ctx, dsn)
		return err
	})
	if err != nil {
		_ = pool.Purge(res)
		return nil, err
	}
	return p, nil
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
}
