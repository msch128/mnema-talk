package messages

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/events/wire"
	"github.com/msch128/mnema-talk/internal/perm"
)

// followSyncTimeout bounds the lookup behind one thread reply.
const followSyncTimeout = 5 * time.Second

// NewFollowSync returns a hub tap that keeps the followed-thread lists of open
// clients current. A reply makes the root's author, the replier and the members
// it mentions follow the thread (see followOnReply), so their sessions are told
// with thread_follow_updated; the clients decide from that list which thread
// replies notify them. Only members who really follow and can still view the
// channel are told.
//
// The lookup runs in the tap, that is before the hub hands the reply to any
// connection, and its updates go out from the same goroutine: a connection
// receives frames in the order they were handed to it, so every session of a
// new follower has thread_follow_updated before the reply it decides on (the
// first reply under one's own message notifies in the page like the push,
// which reads the followers after the commit too). A failed lookup is logged
// and only that reply's update is lost; the next list load shows the follow.
func NewFollowSync(ctx context.Context, p *db.Pool, perms *perm.Service, pub events.Publisher) func(channel uuid.UUID, ev events.Event) {
	perm.Must(perms, "thread follow sync")
	return func(_ uuid.UUID, ev events.Event) {
		created, ok := ev.(*core.MessageCreate)
		if !ok || created == nil || created.ParentID == nil {
			return
		}
		syncFollowers(ctx, p, perms, pub, core.Message(*created))
	}
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
		pub.SendToUsers([]uuid.UUID{user}, wire.ThreadFollowUpdated{ThreadID: thread, ChannelID: msg.ChannelID, Following: true})
	}
	if err := rows.Err(); err != nil {
		slog.Warn("thread follow sync", "err", err)
	}
}
