package worker

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/msch128/mnema-talk/internal/metrics"
)

func TestMain(m *testing.M) {
	slog.SetDefault(slog.New(slog.DiscardHandler))
	os.Exit(m.Run())
}

func TestTickRecoversPanic(t *testing.T) {
	const job = "tick_panic"
	panics := metrics.JobPanics.With(job).Load()
	errs := metrics.JobErrors.With(job).Load()

	Tick(job, func() error { panic("boom") })

	if got := metrics.JobPanics.With(job).Load(); got != panics+1 {
		t.Fatalf("panics = %d, want %d", got, panics+1)
	}
	if got := metrics.JobErrors.With(job).Load(); got != errs {
		t.Fatalf("panic counted as an error: %d, want %d", got, errs)
	}
	if !sawDuration(t, job) {
		t.Fatal("panic did not record a duration")
	}
}

func TestTickCountsError(t *testing.T) {
	const job = "tick_error"
	before := metrics.JobErrors.With(job).Load()
	panics := metrics.JobPanics.With(job).Load()

	Tick(job, func() error { return errors.New("nope") })

	if got := metrics.JobErrors.With(job).Load(); got != before+1 {
		t.Fatalf("errors = %d, want %d", got, before+1)
	}
	if got := metrics.JobPanics.With(job).Load(); got != panics {
		t.Fatalf("error counted as a panic: %d", got)
	}
	if !sawDuration(t, job) {
		t.Fatal("error did not record a duration")
	}
}

func TestTickSuccessRecordsDurationOnly(t *testing.T) {
	const job = "tick_ok"
	errs := metrics.JobErrors.With(job).Load()
	panics := metrics.JobPanics.With(job).Load()

	Tick(job, func() error { return nil })

	if metrics.JobErrors.With(job).Load() != errs || metrics.JobPanics.With(job).Load() != panics {
		t.Fatal("success counted as a failure")
	}
	if !sawDuration(t, job) {
		t.Fatal("success did not record a duration")
	}
}

func TestIgnoreDoneDropsShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if IgnoreDone(ctx, errors.New("db")) != nil {
		t.Fatal("shutdown returned as a job error")
	}
	live := errors.New("db")
	if IgnoreDone(context.Background(), live) != live {
		t.Fatal("live error dropped")
	}
	if IgnoreDone(context.Background(), nil) != nil {
		t.Fatal("nil error became an error")
	}
}

func sawDuration(t *testing.T, job string) bool {
	t.Helper()
	var b strings.Builder
	metrics.WriteInstruments(&b)
	return strings.Contains(b.String(), "mnema_job_duration_seconds_count{job=\""+job+"\"}")
}
