package authn

import (
	"context"
	"log/slog"
	"time"

	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/worker"
)

// RunSessionSweep signs out inactive sessions every interval until ctx ends.
func (h *Handler) RunSessionSweep(ctx context.Context, every time.Duration) {
	if h.Sessions == nil || h.Sessions.DB == nil {
		return
	}
	if every <= 0 {
		every = auth.DefaultSessionSweepEvery
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		worker.Tick(worker.JobSessionSweep, func() error {
			jtis, err := h.Sessions.SweepSessions(ctx)
			if err != nil && ctx.Err() == nil {
				slog.Warn("session inactivity sweep failed", "err", err)
			}
			h.disconnect(jtis)
			return worker.IgnoreDone(ctx, err)
		})
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
