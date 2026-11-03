package auth

import (
	"context"
	"log/slog"
	"time"

	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/worker"
)

// StatusSQLFor yields the status line and its expiry as two select columns of
// the users table (prefix is its alias with a dot, or empty). A status past
// its time reads as empty and without expiry, before the sweep has cleared it.
func StatusSQLFor(prefix string) string {
	live := "(" + prefix + "status_expires_at IS NULL OR " + prefix + "status_expires_at > NOW())"
	return "CASE WHEN " + live + " THEN " + prefix + "status_text ELSE '' END, CASE WHEN " + live + " THEN " + prefix + "status_expires_at END"
}

// statusSweepEvery is how often expired status lines are cleared.
const statusSweepEvery = 30 * time.Second

// RunStatusExpiry clears the status lines whose time has come and announces
// each as user_update, every interval (30s when zero) until ctx ends.
func (h *Handler) RunStatusExpiry(ctx context.Context, interval time.Duration) {
	if h.Sessions == nil || h.Sessions.DB == nil {
		return
	}
	if interval <= 0 {
		interval = statusSweepEvery
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		worker.Tick(worker.JobStatusExpiry, func() error {
			return worker.IgnoreDone(ctx, h.ExpireStatuses(ctx))
		})
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// ExpireStatuses clears the status lines whose time has come. It returns the
// error it logged; a failure to read one row does not drop the others.
func (h *Handler) ExpireStatuses(ctx context.Context) error {
	rows, err := h.Sessions.DB.Query(ctx, `UPDATE users SET status_text = '', status_expires_at = NULL, updated_at = NOW()
		WHERE status_expires_at IS NOT NULL AND status_expires_at <= NOW()
		RETURNING `+userColumns)
	if err != nil {
		slog.Error("status expiry failed", "err", err)
		return err
	}
	// The UPDATE has committed: announce every row that can be read, and say
	// what could not be, instead of leaving clients with a stale status line.
	var cleared []User
	var failed error
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			slog.Error("status expiry: read cleared user", "err", err)
			failed = err
			continue
		}
		cleared = append(cleared, u.Public())
	}
	if err := rows.Err(); err != nil {
		slog.Error("status expiry: iterate cleared users", "err", err)
		failed = err
	}
	rows.Close()
	for _, u := range cleared {
		h.Events.Broadcast(events.UserUpdate, u)
	}
	return failed
}
