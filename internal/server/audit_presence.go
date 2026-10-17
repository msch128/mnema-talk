package server

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/db"
)

// presenceEvent is one join or leave of a Talk, waiting to be written.
type presenceEvent struct {
	user, channel uuid.UUID
	joined        bool
}

// auditPresence returns the hub callback that records who joined and left
// which Talk. The hub must not wait for the database, so events go through a
// bounded queue and a single writer; when it is full the event is dropped and
// logged, which beats stalling voice.
func auditPresence(ctx context.Context, l *audit.Log, q db.Querier) func(user, channel uuid.UUID, joined bool) {
	queue := make(chan presenceEvent, 1024)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case ev := <-queue:
				writePresence(ctx, l, q, ev)
			}
		}
	}()
	return func(user, channel uuid.UUID, joined bool) {
		select {
		case queue <- presenceEvent{user, channel, joined}:
		default:
			slog.Warn("audit log: presence queue full, event dropped", "channel", channel)
		}
	}
}

func writePresence(ctx context.Context, l *audit.Log, q db.Querier, ev presenceEvent) {
	if l == nil {
		return
	}
	var username, channel string
	_ = q.QueryRow(ctx, `SELECT username FROM users WHERE id = $1`, ev.user).Scan(&username)
	_ = q.QueryRow(ctx, `SELECT name FROM channels WHERE id = $1`, ev.channel).Scan(&channel)
	action := "voice.leave"
	if ev.joined {
		action = "voice.join"
	}
	l.RecordOrLog(ctx, audit.Entry{
		ActorID: &ev.user, ActorName: username, Action: action,
		TargetType: "channel", TargetID: ev.channel.String(), TargetName: channel,
	})
}
