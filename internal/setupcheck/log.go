package setupcheck

import (
	"context"
	"log/slog"
)

// Log writes the checks to the log: one line per amber or red check with the
// fix, and a summary line, so the same list is in `docker compose logs`.
func Log(logger *slog.Logger, checks []Check) {
	problems := 0
	for _, c := range checks {
		if c.Level == OK {
			continue
		}
		problems++
		level := slog.LevelWarn
		if c.Level == Fail {
			level = slog.LevelError
		}
		logger.Log(context.Background(), level, "setup check: "+c.Detail, "check", c.ID, "status", string(c.Level), "fix", c.Fix)
	}
	logger.Info("setup checks", "ok", len(checks)-problems, "attention", problems, "worst", string(Worst(checks)))
}
