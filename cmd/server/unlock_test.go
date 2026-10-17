package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
)

func TestUnlockCommandUsage(t *testing.T) {
	for _, args := range [][]string{nil, {"a", "b"}, {"--all", "max"}, {"--bogus"}} {
		err := unlockCommand(context.Background(), args, &bytes.Buffer{})
		if err == nil || !strings.Contains(err.Error(), "usage: mnema-talk unlock") {
			t.Errorf("args %v: err = %v", args, err)
		}
	}
}

func TestUnlockCommandNeedsConfiguration(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	if err := unlockCommand(context.Background(), []string{"max"}, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Fatalf("err = %v", err)
	}
}

func TestReorderFlags(t *testing.T) {
	got := reorderFlags([]string{"max", "--all"})
	if !reflect.DeepEqual(got, []string{"--all", "max"}) {
		t.Fatalf("got %v", got)
	}
}

func TestRunDispatchesUnlockSubcommand(t *testing.T) {
	old := os.Args
	t.Cleanup(func() { os.Args = old })
	os.Args = []string{"mnema-talk", "unlock"}
	if err := run(); err == nil || !strings.Contains(err.Error(), "usage: mnema-talk unlock") {
		t.Fatalf("run = %v", err)
	}
}

type fakeRow struct{ scan func(dest ...any) error }

func (r fakeRow) Scan(dest ...any) error { return r.scan(dest...) }

// fakeUnlockDB answers the statements requestUnlock issues.
type fakeUnlockDB struct {
	db.Querier
	failInsert  bool
	failPending bool
}

func (f fakeUnlockDB) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	return fakeRow{func(dest ...any) error {
		switch {
		case strings.Contains(sql, "INSERT"):
			if f.failInsert {
				return errors.New("insert failed")
			}
			*(dest[0].(*int64)) = 7
		case strings.Contains(sql, "EXISTS"):
			if f.failPending {
				return errors.New("db down")
			}
			*(dest[0].(*bool)) = true
		}
		return nil
	}}
}

func TestRequestUnlockReportsDatabaseAndContextErrors(t *testing.T) {
	var out bytes.Buffer
	if err := requestUnlock(context.Background(), fakeUnlockDB{failPending: true}, "", true, &out); err == nil || !strings.Contains(err.Error(), "db down") {
		t.Fatalf("pending error: %v", err)
	}
	if err := requestUnlock(context.Background(), fakeUnlockDB{failInsert: true}, "", true, &out); err == nil || !strings.Contains(err.Error(), "insert failed") {
		t.Fatalf("insert error: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	old := unlockWait
	unlockWait = time.Minute
	t.Cleanup(func() { unlockWait = old })
	if err := requestUnlock(ctx, fakeUnlockDB{}, "", true, &out); !errors.Is(err, context.Canceled) {
		t.Fatalf("context error: %v", err)
	}
}
