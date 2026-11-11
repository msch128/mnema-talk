package main

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/msch128/mnema-talk/internal/auth"
)

// stuckDatabase answers no query before the caller gives up.
type stuckDatabase struct{}

func (stuckDatabase) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}

func (stuckDatabase) QueryRow(context.Context, string, ...any) pgx.Row { return nil }

func (stuckDatabase) Query(ctx context.Context, _ string, _ ...any) (pgx.Rows, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

// The check of the stored authenticator secrets runs before the server
// listens. A database that does not answer it must not hold the start up:
// the check gives up after its own limit, says so and the start goes on.
func TestTheStartDoesNotWaitForASlowCheckOfTheStoredSecrets(t *testing.T) {
	var log bytes.Buffer
	done := make(chan struct{})
	go func() {
		defer close(done)
		s := &auth.Sessions{Secret: strings.Repeat("j", 40)}
		reportStoredSecrets(context.Background(), s, stuckDatabase{}, slog.New(slog.NewJSONHandler(&log, nil)), 20*time.Millisecond)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the start waited for a database that does not answer the check")
	}
	for _, want := range []string{`"level":"WARN"`, "could not check the stored authenticator secrets", "deadline exceeded"} {
		if !strings.Contains(log.String(), want) {
			t.Errorf("the log lacks %q: %s", want, log.String())
		}
	}
}
