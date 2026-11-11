// Package reportcount counts the open reports and tells the moderators when the
// number changes. Reports are filed by members (internal/chat/reports) and by
// automod; both announce through here.
package reportcount

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/perm"
)

// Open is how many reports are open.
func Open(ctx context.Context, q db.Querier) (int, error) {
	var n int
	if err := q.QueryRow(ctx, `SELECT COUNT(*) FROM message_reports WHERE status = 'open'`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count open reports: %w", err)
	}
	return n, nil
}

// Announce sends the current open count to the sessions of members who hold
// Manage reports. A failure is logged: the queue itself is already stored, and
// the badge catches up with the next change or reload. A nil pub does nothing.
func Announce(ctx context.Context, q db.Querier, pub events.Publisher) {
	if pub == nil {
		return
	}
	n, err := Open(ctx, q)
	if err != nil {
		slog.Warn("open report count not announced", "err", err)
		return
	}
	pub.BroadcastPerm(perm.ManageReports, events.Untyped(events.ReportsOpen, map[string]int{"open": n}))
}
