package authn

import (
	"context"
	"testing"
	"time"
)

// Drain (shutdown) waits on a sync.WaitGroup while handlers that outlive the
// shutdown timeout can still start jobs. These tests pin what happens to a job
// that starts after Drain began or returned. Nothing refuses it: the job runs
// (in production against a closed pool). That is candidate C-L7-a, recorded
// as a known failure written as an inverse assertion: each test passes while
// the gap exists and turns red the day a job is refused after Drain, which is
// the day of the fix.

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

func TestKnownGapMailJobStartsAfterDrainReturned(t *testing.T) {
	h := &Handler{rec: newRecovery()}
	if !h.Drain(time.Second) {
		t.Fatal("an idle handler must drain at once")
	}
	if !jobRuns(h.background) {
		t.Fatal("fixed: a mail job after Drain is refused; turn this into a regression test")
	}
	h.DrainMail()
}

func TestKnownGapMailJobStartsWhileDrainTimedOut(t *testing.T) {
	h := &Handler{rec: newRecovery()}
	release := make(chan struct{})
	h.background(func(context.Context) { <-release }) // the handler stuck past the timeout
	if h.Drain(20 * time.Millisecond) {
		t.Fatal("Drain reported done with a job in flight")
	}
	// Drain gave up, the process proceeds to close the pool; a late handler
	// starts another job.
	if !jobRuns(h.background) {
		t.Fatal("fixed: a mail job after a timed-out Drain is refused; turn this into a regression test")
	}
	close(release)
	h.DrainMail()
}

func TestKnownGapTrackedJobAcceptedAfterDrainReturned(t *testing.T) {
	h := &Handler{rec: newRecovery()}
	if !h.Drain(time.Second) {
		t.Fatal("an idle handler must drain at once")
	}
	done := h.TrackJob() // an export started after Drain: no way to refuse it
	if h.Drain(20 * time.Millisecond) {
		t.Fatal("fixed: a job tracked after Drain is not counted any more; turn this into a regression test")
	}
	done()
	if !h.Drain(time.Second) {
		t.Fatal("Drain must finish once the tracked job is done")
	}
}
