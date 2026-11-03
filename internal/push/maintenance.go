package push

import (
	"context"
	"log/slog"

	"github.com/msch128/mnema-talk/internal/worker"
)

// sweepMaintenance runs one periodic sweep: drop coalesced pushes and the
// devices of sessions that have ended. Delivery itself is not part of it.
func (s *Service) sweepMaintenance(ctx context.Context) {
	worker.Tick(worker.JobPushSweep, func() error {
		s.co.sweep()
		return worker.IgnoreDone(ctx, s.pruneEnded(ctx))
	})
}

// pruneEnded removes the devices of sessions that were signed out or expired,
// or that a password change or "log out everywhere" revoked. They are never
// told anything meanwhile (every query checks the session), this only tidies up.
func (s *Service) pruneEnded(ctx context.Context) error {
	n, err := pruneEnded(ctx, s.DB)
	if err != nil && ctx.Err() == nil {
		slog.Warn("push: prune ended sessions", "err", err)
	} else if n > 0 {
		slog.Debug("push: removed devices of ended sessions", "count", n)
	}
	return err
}
