//go:build integration

// Package testutil provides the shared Postgres fixture for integration tests
// (build tag "integration"), modelled on mnema.xyz's internal/testutil.
//
// A single postgres:17-alpine container is started per test binary via
// dockertest and migrated with the embedded migrations. Set
// TEST_DATABASE_URL to use an existing database instead (CI service
// container). Tests isolate themselves with Reset, which truncates all data.
// Packages using DB call Main from TestMain so the container is removed.
package testutil

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/ory/dockertest/v4"
)

var (
	startOnce  sync.Once
	shared     *db.Pool
	startErr   error
	dockerPool dockertest.ClosablePool
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

	pool, err := dockertest.NewPool(ctx, "", dockertest.WithMaxWait(90*time.Second))
	if err != nil {
		return nil, fmt.Errorf("dockertest pool: %w", err)
	}

	res, err := pool.Run(ctx, "postgres",
		dockertest.WithTag("17-alpine"),
		dockertest.WithEnv([]string{"POSTGRES_USER=mnema", "POSTGRES_PASSWORD=mnema", "POSTGRES_DB=mnema_test"}),
		dockertest.WithLabels(map[string]string{"mnema-talk.testutil": "postgres"}),
		dockertest.WithoutReuse(),
		dockertest.WithHostConfig(func(hc *container.HostConfig) {
			hc.AutoRemove = true
			hc.RestartPolicy = container.RestartPolicy{Name: container.RestartPolicyDisabled}
		}),
	)
	if err != nil {
		_ = pool.Close(context.Background())
		return nil, fmt.Errorf("run postgres: %w", err)
	}

	dsn := fmt.Sprintf("postgres://mnema:mnema@%s/mnema_test?sslmode=disable", res.GetHostPort("5432/tcp"))
	var p *db.Pool
	err = pool.Retry(ctx, 0, func() error {
		var err error
		p, err = connectAndMigrate(ctx, dsn)
		return err
	})
	if err != nil {
		_ = pool.Close(context.Background())
		return nil, err
	}
	dockerPool = pool
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
	if dockerPool != nil {
		if err := dockerPool.Close(context.Background()); err != nil {
			fmt.Fprintf(os.Stderr, "testutil: remove postgres container: %v\n", err)
		}
	}
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
}
