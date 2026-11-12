package authn

import (
	"context"
	"testing"
	"time"
)

// Drain (shutdown) waits on a sync.WaitGroup while handlers that outlive the
// shutdown timeout can still start jobs. From the start of Drain on, every new
// job is refused: it would run against a closing database pool, and an Add
// concurrent with the Wait is a WaitGroup misuse that ends the process
// (H-L7-08).

// jobRuns starts a job through start and reports whether it ran.
func jobRuns(start func(func(context.Context))) bool {
	ran := make(chan struct{})
	start(func(context.Context) { close(ran) })
	select {
	case <-ran:
		return true
	case <-time.After(time.Second):
		return false
	}
}

func TestMailJobIsRefusedAfterDrainReturned(t *testing.T) {
	h := &Handler{rec: newRecovery()}
	if !h.Drain(time.Second) {
		t.Fatal("an idle handler must drain at once")
	}
	if jobRuns(h.background) {
		t.Fatal("a mail job started after Drain")
	}
	if n := len(h.rec.slots); n != 0 {
		t.Fatalf("a refused mail job kept its queue slot: %d taken", n)
	}
}

func TestMailJobIsRefusedAfterDrainTimedOut(t *testing.T) {
	h := &Handler{rec: newRecovery()}
	release := make(chan struct{})
	h.background(func(context.Context) { <-release }) // the handler stuck past the timeout
	if h.Drain(20 * time.Millisecond) {
		t.Fatal("Drain reported done with a job in flight")
	}
	// Drain gave up and the process proceeds to close the pool; a late handler
	// must not start another job.
	if jobRuns(h.background) {
		t.Fatal("a mail job started after a timed-out Drain")
	}
	close(release)
	if !h.Drain(time.Second) {
		t.Fatal("Drain must finish once the stuck job is done")
	}
}

func TestTrackedJobIsRefusedAfterDrainReturned(t *testing.T) {
	h := &Handler{rec: newRecovery()}
	if !h.Drain(time.Second) {
		t.Fatal("an idle handler must drain at once")
	}
	if done, ok := h.TrackJob(); ok || done != nil {
		t.Fatal("a job was tracked after Drain")
	}
	if !h.Drain(20 * time.Millisecond) {
		t.Fatal("a refused job must not hold up Drain")
	}
}

func TestTrackedJobBeforeDrainIsWaitedFor(t *testing.T) {
	h := &Handler{rec: newRecovery()}
	done, ok := h.TrackJob()
	if !ok {
		t.Fatal("a job before Drain must be tracked")
	}
	if h.Drain(20 * time.Millisecond) {
		t.Fatal("Drain returned with a tracked job in flight")
	}
	done()
	if !h.Drain(time.Second) {
		t.Fatal("Drain must finish once the tracked job is done")
	}
}
