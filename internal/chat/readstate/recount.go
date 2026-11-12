package readstate

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/events/wire"
	"github.com/msch128/mnema-talk/internal/perm"
)

// Online reports who has an open connection right now (the WebSocket hub).
type Online interface {
	OnlineUserIDs() []uuid.UUID
}

// Recount tells members their counts of a channel again after messages there
// were deleted or edited. The counts are computed from the messages that exist
// now and their current mentions, and neither a delete nor an edit comes with
// a message a client could count, so the server, which knows the counts, sends
// them (read_state with unread_count and mention_count). The zero value tells
// nobody.
type Recount struct {
	DB     *db.Pool
	Perms  *perm.Service
	Online Online
	Events events.Publisher
}

// Tell sends the counts of channel to the members whose badge the change may
// have moved: those among candidates (every member when nil) with an open
// connection who may view the channel and had not read it up to since (the
// newest changed message; nil means now). A member who had read it was not
// counting the message. A failure is logged: the badge then heals at the next
// read, reconnect or reload, as it did before.
func (r Recount) Tell(ctx context.Context, channel uuid.UUID, since *time.Time, candidates []uuid.UUID) {
	if r.DB == nil || r.Events == nil || r.Perms == nil {
		return
	}
	users := r.online(candidates)
	if len(users) == 0 {
		return
	}
	if err := r.tell(ctx, channel, since, users); err != nil {
		slog.Warn("read state recount", "channel", channel, "err", err)
	}
}

// online keeps the candidates with an open connection; every connected member
// when candidates is nil.
func (r Recount) online(candidates []uuid.UUID) []uuid.UUID {
	if r.Online == nil {
		return candidates
	}
	connected := r.Online.OnlineUserIDs()
	if candidates == nil {
		return connected
	}
	want := make(map[uuid.UUID]bool, len(candidates))
	for _, id := range candidates {
		want[id] = true
	}
	out := make([]uuid.UUID, 0, len(candidates))
	for _, id := range connected {
		if want[id] {
			out = append(out, id)
			delete(want, id) // a member with two connections is told once
		}
	}
	return out
}

func (r Recount) tell(ctx context.Context, channel uuid.UUID, since *time.Time, users []uuid.UUID) error {
	behind, err := readBefore(ctx, r.DB, channel, since, users)
	if err != nil || len(behind) == 0 {
		return err
	}
	// Fail closed: without the current permissions nobody is told.
	snap, err := r.Perms.Snapshot(ctx)
	if err != nil {
		return err
	}
	for _, user := range behind {
		if !snap.Channel(perm.Subject{ID: user}, channel).Has(perm.ViewChannel) {
			continue
		}
		state, ok, err := ChannelReadState(ctx, r.DB, user, channel)
		if err != nil {
			return err
		}
		if ok {
			r.Events.SendToUsers([]uuid.UUID{user}, wire.ReadStateCounts{
				ChannelID: channel, LastReadAt: state.LastReadAt, MentionCount: state.MentionCount, UnreadCount: state.UnreadCount,
			})
		}
	}
	return nil
}

// readBefore keeps the users whose read marker in channel (their sign-up when
// they never read it) lies before since (the database's now when nil).
func readBefore(ctx context.Context, q db.Querier, channel uuid.UUID, since *time.Time, users []uuid.UUID) ([]uuid.UUID, error) {
	rows, err := q.Query(ctx, `
		SELECT u.id FROM users u
		LEFT JOIN channel_reads cr ON cr.user_id = u.id AND cr.channel_id = $1
		WHERE u.id = ANY($2) AND COALESCE(cr.last_read_at, u.created_at) < COALESCE($3, clock_timestamp())`, channel, users, since)
	if err != nil {
		return nil, fmt.Errorf("read markers: %w", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, fmt.Errorf("read markers: %w", err)
	}
	return ids, nil
}

// ChannelReadState is GetReadStates for one channel; false when the channel
// has no read state there (a conversation, or a channel that is gone).
func ChannelReadState(ctx context.Context, q db.Querier, userID, channelID uuid.UUID) (ReadState, bool, error) {
	rows, err := q.Query(ctx, `SELECT * FROM (`+ReadStateSQL()+`) rs WHERE rs.id = $3`, userID, core.MentionKindGroup, channelID)
	if err != nil {
		return ReadState{}, false, fmt.Errorf("read state: %w", err)
	}
	states, err := scanReadStates(rows)
	if err != nil || len(states) == 0 {
		return ReadState{}, false, err
	}
	return states[0], true, nil
}
