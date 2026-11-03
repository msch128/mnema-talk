// Package worker runs one iteration of a periodic maintenance job.
// A panic is recovered so the caller's loop keeps its schedule; the job's
// duration, errors and panics are metrics (mnema_job_*).
package worker

import (
	"context"
	"log/slog"
	"runtime/debug"
	"time"

	"github.com/msch128/mnema-talk/internal/metrics"
)

// Job label values for the maintenance metrics. Each is one periodic sweep.
const (
	JobSessionSweep    = "session_sweep"
	JobStatusExpiry    = "status_expiry"
	JobBanExpiry       = "ban_expiry"
	JobUnlock          = "unlock"
	JobExportSweep     = "export_sweep"
	JobSlowMode        = "slow_mode_prune"
	JobEditHistory     = "edit_history"
	JobAutoDelete      = "auto_delete"
	JobPollFold        = "poll_fold"
	JobLater           = "later"
	JobAuditRetention  = "audit_retention"
	JobCommunityEvents = "community_events"
	JobPushSweep       = "push_sweep"
	JobAvatarReencode  = "avatar_reencode"
)

// Tick runs fn and always records its duration under job.
// A non-nil error increments the error counter. A panic is logged, counted
// on its own (not also as an error) and recovered, so Tick returns.
func Tick(job string, fn func() error) {
	start := time.Now()
	defer func() {
		metrics.JobDuration.Observe(job, time.Since(start))
	}()
	defer func() {
		rec := recover()
		if rec == nil {
			return
		}
		metrics.JobPanics.With(job).Inc()
		slog.Error("maintenance job panicked", "job", job, "err", rec, "stack", string(debug.Stack()))
	}()
	if err := fn(); err != nil {
		metrics.JobErrors.With(job).Inc()
	}
}

// IgnoreDone turns err into nil once ctx has ended. Stopping the process is
// not a failed run.
func IgnoreDone(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return nil
	}
	return err
}
