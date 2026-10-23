package chat

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/perm"
)

// followSyncTimeout bounds the lookup behind one thread reply.
const followSyncTimeout = 5 * time.Second

// NewFollowSync returns a hub tap that keeps the followed-thread lists of open
// clients current. A reply makes the root's author, the replier and the members
// it mentions follow the thread (see followOnReply), so their sessions are told
// with thread_follow_updated; the clients decide from that list which thread
// replies notify them. Only members who really follow are told, and members who can no longer view the channel are
// skipped, and the lookup runs off the publishing path.
func NewFollowSync(ctx context.Context, p *db.Pool, perms *perm.Service, pub events.Publisher) func(channel uuid.UUID, eventType string, payload any) {
	return func(_ uuid.UUID, eventType string, payload any) {
		if eventType != events.MessageCreate {
			return
		}
		msg, ok := messageOf(payload)
		if !ok || msg.ParentID == nil {
			return
		}
		go syncFollowers(ctx, p, perms, pub, msg)
	}
}

func messageOf(payload any) (Message, bool) {
	switch v := payload.(type) {
	case Message:
		return v, true
	case *Message:
		if v != nil {
			return *v, true
		}
	}
	return Message{}, false
}

func syncFollowers(parent context.Context, p *db.Pool, perms *perm.Service, pub events.Publisher, msg Message) {
	ctx, cancel := context.WithTimeout(parent, followSyncTimeout)
	defer cancel()
	thread := *msg.ParentID
	// Fail closed: without the current permissions nobody is told.
	if perms == nil {
		return
	}
	snap, err := perms.Snapshot(ctx)
	if err != nil {
		slog.Warn("thread follow sync: permissions", "err", err)
		return
	}
	rows, err := p.Query(ctx, `
		SELECT f.user_id FROM thread_follows f
		WHERE f.thread_id = $1 AND f.following AND (
			f.user_id = $2
			OR f.user_id = (SELECT user_id FROM messages WHERE id = $1)
			OR f.user_id IN (SELECT user_id FROM message_mentions WHERE message_id = $3 AND kind <> 1))`,
		thread, msg.UserID, msg.ID)
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
}
