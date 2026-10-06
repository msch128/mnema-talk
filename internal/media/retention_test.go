package media

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunPeriodicallyRepeatsUntilCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var calls atomic.Int32
	done := make(chan struct{})
	go func() {
		runPeriodically(ctx, time.Millisecond, time.Millisecond, func(context.Context) {
			if calls.Add(1) == 3 {
				cancel()
			}
		})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("runPeriodically did not stop after cancel")
	}
	if n := calls.Load(); n != 3 {
		t.Fatalf("fn ran %d times, want 3", n)
	}
}

func TestRunPeriodicallyWaitsForFirstRun(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	ran := false
	runPeriodically(ctx, time.Hour, time.Hour, func(context.Context) { ran = true })
	if ran {
		t.Fatal("fn ran before the first delay elapsed")
	}
}

func TestRetentionWorkerIsOffByDefault(t *testing.T) {
	// Days 0 (the default) or no store: nothing is started, so a nil pool is
	// never touched.
	StartRetentionWorker(context.Background(), nil, NewMemoryStore(), 0)
	StartRetentionWorker(context.Background(), nil, nil, 30)
}
