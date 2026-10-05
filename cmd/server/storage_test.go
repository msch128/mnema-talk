package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/media"
)

func TestLazyStoreRetriesUntilStorageIsUp(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var attempts atomic.Int32
	mem := media.NewMemoryStore()
	l := startLazyStore(ctx, func(context.Context) (media.Store, error) {
		if attempts.Add(1) < 3 {
			return nil, errors.New("connection refused")
		}
		return mem, nil
	}, time.Millisecond, 4*time.Millisecond)

	if l.Ready() == nil && attempts.Load() < 3 {
		t.Fatal("ready before init succeeded")
	}
	// Until ready, calls fail with 503 instead of a generic 500.
	if err := l.Upload(ctx, "k", bytes.NewReader(nil), "text/plain", 0); err != nil {
		if ae, ok := httpx.AsAPIError(err); !ok || ae.Status != http.StatusServiceUnavailable {
			t.Fatalf("not ready: err = %v", err)
		}
	}

	deadline := time.Now().Add(2 * time.Second)
	for l.Ready() != nil {
		if time.Now().After(deadline) {
			t.Fatalf("store never became ready: %v", l.Ready())
		}
		time.Sleep(time.Millisecond)
	}
	if err := l.Upload(ctx, "k", bytes.NewReader([]byte("x")), "text/plain", 1); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := l.GetObject(ctx, "k"); err != nil {
		t.Fatal(err)
	}
	if attempts.Load() != 3 {
		t.Fatalf("attempts = %d, want 3", attempts.Load())
	}
}

func TestLazyStoreStopsRetryingWithContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var attempts atomic.Int32
	l := startLazyStore(ctx, func(context.Context) (media.Store, error) {
		attempts.Add(1)
		return nil, errors.New("down")
	}, time.Millisecond, time.Millisecond)
	cancel()
	time.Sleep(20 * time.Millisecond)
	n := attempts.Load()
	time.Sleep(20 * time.Millisecond)
	if attempts.Load() != n {
		t.Fatal("still retrying after the context ended")
	}
	if l.Ready() == nil {
		t.Fatal("ready without a store")
	}
}
