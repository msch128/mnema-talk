package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/msch128/mnema-talk/internal/config"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/media"
)

func TestLazyStoreMethodsUnavailableAndDelegated(t *testing.T) {
	ctx := context.Background()
	unavailable := &lazyStore{err: errStorageStarting}
	for name, invoke := range map[string]func() error{
		"upload": func() error { return unavailable.Upload(ctx, "key", bytes.NewReader(nil), "text/plain", 0) },
		"get":    func() error { _, err := unavailable.GetObjectFrom(ctx, "key", 0); return err },
		"delete": func() error { return unavailable.Delete(ctx, "key") },
		"batch":  func() error { return unavailable.DeleteBatch(ctx, []string{"key"}) },
		"list":   func() error { return unavailable.List(ctx, "", func(media.ObjectInfo) error { return nil }) },
	} {
		if ae, ok := httpx.AsAPIError(invoke()); !ok || ae.Status != 503 {
			t.Fatalf("%s: %v", name, ae)
		}
	}
	store := media.NewMemoryStore()
	l := startLazyStore(ctx, func(context.Context) (media.Store, error) { return store, nil }, time.Millisecond, time.Millisecond)
	for _, key := range []string{"keep", "delete", "batch"} {
		if err := l.Upload(ctx, key, bytes.NewBufferString("value"), "text/plain", 5); err != nil {
			t.Fatal(err)
		}
	}
	if err := l.Delete(ctx, "delete"); err != nil {
		t.Fatal(err)
	}
	if err := l.DeleteBatch(ctx, []string{"batch"}); err != nil {
		t.Fatal(err)
	}
	var keys []string
	if err := l.List(ctx, "", func(obj media.ObjectInfo) error { keys = append(keys, obj.Key); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || keys[0] != "keep" {
		t.Fatal(keys)
	}
	f, err := l.GetObjectFrom(ctx, "keep", 1)
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	want := errors.New("stop listing")
	if err := l.List(ctx, "", func(media.ObjectInfo) error { return want }); !errors.Is(err, want) {
		t.Fatal(err)
	}
}

func TestLogLevel(t *testing.T) {
	for _, tc := range []struct {
		level, env string
		want       slog.Level
	}{
		{"debug", "production", slog.LevelDebug}, {"info", "development", slog.LevelInfo},
		{"warn", "development", slog.LevelWarn}, {"error", "development", slog.LevelError},
		{"", "production", slog.LevelInfo}, {"", "development", slog.LevelDebug},
	} {
		if got := logLevel(&config.Config{LogLevel: tc.level, AppEnv: tc.env}); got != tc.want {
			t.Fatalf("%+v = %v", tc, got)
		}
	}
}

func TestRunRejectsInvalidConfiguration(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	if err := run(); err == nil {
		t.Fatal("invalid config accepted")
	}
}
