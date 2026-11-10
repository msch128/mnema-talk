package messages

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/safego"
)

// followSyncTimeout bounds the lookup behind one thread reply.
const followSyncTimeout = 5 * time.Second

const (
	// followSyncWorkers run the lookups side by side, so a burst of replies
	// cannot take the pool's connections from the request handlers.
	followSyncWorkers = 4
	// followSyncQueue is how many replies wait for a worker. A reply beyond
	// it is skipped: the follow itself is stored, only the live update to
	// open clients is lost and the next list load shows it.
	followSyncQueue = 256
)

// NewFollowSync returns a hub tap that keeps the followed-thread lists of open
// clients current. A reply makes the root's author, the replier and the members
// it mentions follow the thread (see followOnReply), so their sessions are told
// with thread_follow_updated; the clients decide from that list which thread
// replies notify them. Only members who really follow and can still view the
// channel are told. The lookup runs off the publishing path, so a failed one
// is logged and that reply's update is lost; the next list load shows the follow.
func NewFollowSync(ctx context.Context, p *db.Pool, perms *perm.Service, pub events.Publisher) func(channel uuid.UUID, eventType string, payload any) {
	perm.Must(perms, "thread follow sync")
	queue := make(chan core.Message, followSyncQueue)
	for range followSyncWorkers {
		safego.Go("thread_follow_sync", safego.Crash, func() {
			for {
				select {
				case <-ctx.Done():
					return
				case msg := <-queue:
					syncFollowers(ctx, p, perms, pub, msg)
				}
			}
		})
	}
	return func(_ uuid.UUID, eventType string, payload any) {
		if eventType != events.MessageCreate {
			return
		}
		msg, ok := MessageOf(payload)
		if !ok || msg.ParentID == nil {
			return
		}
		select {
		case queue <- msg:
		default:
			slog.Warn("thread follow sync: queue full, reply skipped", "thread", *msg.ParentID)
		}
	}
}

func MessageOf(payload any) (core.Message, bool) {
	switch v := payload.(type) {
	case core.Message:
		return v, true
	case *core.Message:
		if v != nil {
			return *v, true
		}
	}
	return core.Message{}, false
}

func syncFollowers(parent context.Context, p *db.Pool, perms *perm.Service, pub events.Publisher, msg core.Message) {
	ctx, cancel := context.WithTimeout(parent, followSyncTimeout)
	defer cancel()
	thread := *msg.ParentID
	// Fail closed: without the current permissions nobody is told.
	snap, err := perms.Snapshot(ctx)
	if err != nil {
		slog.Warn("thread follow sync: permissions", "err", err)
		return
	}
	rows, err := p.Query(ctx, `
		SELECT f.user_id FROM thread_follows f
		WHERE f.thread_id = $1 AND f.following AND f.user_id IN (`+replyFollowersSQL+`)`,
		thread, msg.UserID, msg.ID, core.MentionKindGroup)
	if err != nil {
		slog.Warn("thread follow sync", "err", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var user uuid.UUID
		if err := rows.Scan(&user); err != nil {
			slog.Warn("thread follow sync", "err", err)
			return
		}
		// A member who lost the channel keeps no view of its threads.
		if !snap.Channel(perm.Subject{ID: user}, msg.ChannelID).Has(perm.ViewChannel) {
			continue
		}
		pub.SendToUsers([]uuid.UUID{user}, events.ThreadFollowUpdated, map[string]any{
			"thread_id": thread, "channel_id": msg.ChannelID, "following": true,
		})
	}
	if err := rows.Err(); err != nil {
		slog.Warn("thread follow sync", "err", err)
	}
}
