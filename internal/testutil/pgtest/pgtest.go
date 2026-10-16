//go:build integration

// Package pgtest starts the throwaway Postgres container integration tests
// share. It deliberately does not import internal/db, so the db package's own
// tests can use it; internal/testutil builds the migrated pool on top of it.
package pgtest

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moby/moby/api/types/container"
	"github.com/ory/dockertest/v4"
)

var (
	startOnce  sync.Once
	dsn        string
	startErr   error
	dockerPool dockertest.ClosablePool
)

// DSN returns a connection string for a test database: TEST_DATABASE_URL when
// set (CI service container), otherwise a postgres:18-alpine container
// started on first use via dockertest. The container is shared by the whole
// test binary; call Stop from TestMain to remove it.
func DSN() (string, error) {
	startOnce.Do(func() { dsn, startErr = start() })
	return dsn, startErr
}

func start() (string, error) {
	if env := os.Getenv("TEST_DATABASE_URL"); env != "" {
		return env, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	pool, err := dockertest.NewPool(ctx, "", dockertest.WithMaxWait(90*time.Second))
	if err != nil {
		return "", fmt.Errorf("dockertest pool: %w", err)
	}
	res, err := pool.Run(ctx, "postgres",
		dockertest.WithTag("18-alpine"),
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
		return "", fmt.Errorf("run postgres: %w", err)
	}
	dockerPool = pool
	url := fmt.Sprintf("postgres://mnema:mnema@%s/mnema_test?sslmode=disable", res.GetHostPort("5432/tcp"))
	// The image restarts Postgres once after its init scripts, so the port
	// accepts connections before the server is really ready.
	if err := pool.Retry(ctx, 0, func() error { return ping(ctx, url) }); err != nil {
		_ = pool.Close(context.Background())
		dockerPool = nil
		return "", fmt.Errorf("wait for postgres: %w", err)
	}
	return url, nil
}

func ping(ctx context.Context, url string) error {
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	return conn.Ping(ctx)
}

// Stop removes the container, if one was started.
func Stop() {
	if dockerPool == nil {
		return
	}
	// AutoRemove makes the daemon delete the container as it stops, so the
	// explicit removal can find it already going away.
	if err := dockerPool.Close(context.Background()); err != nil && !strings.Contains(err.Error(), "already in progress") {
		fmt.Fprintf(os.Stderr, "pgtest: remove postgres container: %v\n", err)
	}
}
