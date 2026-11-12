package authn

import (
	"context"
	"testing"
	"time"

	"github.com/msch128/mnema-talk/internal/metrics"
)

// TestPanicInAMailJobFreesItsSlot fires the panic the account_mail site
// abandons and checks what it leaves: the queue slot and the wait group are
// released, so later mail still goes out and DrainMail returns.
func TestPanicInAMailJobFreesItsSlot(t *testing.T) {
	h := &Handler{rec: newRecovery()}
	before := metrics.GoroutinePanics.With("account_mail").Load()
	for range 2 * mailJobs {
		h.background(func(context.Context) { panic("mail job failed") })
		waitDrained(t, h)
	}
	// A panic is counted after the job's own deferred calls, so after the wait
	// group is done: the last count can land a moment after the drain returns.
	counted := func() int64 { return int64(metrics.GoroutinePanics.With("account_mail").Load() - before) }
	for deadline := time.Now().Add(5 * time.Second); counted() != 2*mailJobs && time.Now().Before(deadline); {
		time.Sleep(time.Millisecond)
	}
	if got := counted(); got != 2*mailJobs {
		t.Fatalf("panics counted: %d, want %d", got, 2*mailJobs)
	}
	if n := len(h.rec.slots); n != 0 {
		t.Fatalf("%d queue slots still taken after the panics", n)
	}

	// Every slot is usable again: mailJobs jobs run at once.
	ran, release := make(chan struct{}, mailJobs), make(chan struct{})
	for range mailJobs {
		h.background(func(context.Context) { ran <- struct{}{}; <-release })
	}
	for i := range mailJobs {
		select {
		case <-ran:
		case <-time.After(5 * time.Second):
			t.Fatalf("only %d of %d mail jobs started after the panics", i, mailJobs)
		}
	}
	close(release)
	waitDrained(t, h)
}

func waitDrained(t *testing.T, h *Handler) {
	t.Helper()
	done := make(chan struct{})
	go func() { h.DrainMail(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the mail jobs did not finish")
	}
}
