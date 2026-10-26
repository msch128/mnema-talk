package chat

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

const (
	// DefaultTalkEmptyGrace is how long a temporary Talk may stay empty before
	// it is removed. The hub already keeps a dropped member listed for its own
	// grace period, so this only covers an explicit leave and a Talk nobody
	// entered.
	DefaultTalkEmptyGrace = 30 * time.Second
	// TalksPerWindow members may open per TalkWindow.
	TalksPerWindow = 5
	TalkWindow     = 10 * time.Minute
	// TalkRequestsPerMinute caps edits and invitations per member.
	TalkRequestsPerMinute = 30
)

// TalkManager keeps the live side of temporary Talks: who is in them, who
// leads, and when an empty one goes away. It learns about members from the
// hub's join/leave hook (OnPresence) and never touches the SFU.
type TalkManager struct {
	DB     *db.Pool
	Events events.Publisher
	Perms  *perm.Service
	// Audit records the removal of an empty Talk (by "system"); nil records nothing.
	Audit *audit.Log
	// Objects deletes the media of a removed Talk's chat; nil skips it.
	Objects ObjectDeleter
	// Voice disconnects stragglers from a removed Talk's room; nil skips it.
	Voice VoiceRooms
	// CreateLimit caps the Talks one member opens (TalksPerWindow per
	// TalkWindow); nil means no cap.
	CreateLimit *httpx.RateLimiter
	// EmptyGrace is how long an empty Talk lives on; zero means the default.
	EmptyGrace time.Duration

	mu    sync.Mutex
	talks map[uuid.UUID]*liveTalk
	// pending holds the join and leave events not applied yet, oldest first.
	// It never drops one: a lost join would let an occupied Talk be removed,
	// a lost leave would keep an empty one forever.
	pending []presenceEvent
	wake    chan struct{}
}

// liveTalk is what the manager knows of one open temporary Talk.
type liveTalk struct {
	// members are the members in the Talk, longest present first.
	members []uuid.UUID
	leader  *uuid.UUID
	timer   *time.Timer
}

type presenceEvent struct {
	user, channel uuid.UUID
	joined        bool
}

// NewTalkManager readies m for use; Run starts it.
func NewTalkManager(m *TalkManager) *TalkManager {
	m.talks = map[uuid.UUID]*liveTalk{}
	m.wake = make(chan struct{}, 1)
	if m.EmptyGrace <= 0 {
		m.EmptyGrace = DefaultTalkEmptyGrace
	}
	return m
}

// Run removes the temporary Talks a restart left behind (nobody is in a Talk
// after a restart) and then follows the hub's join and leave events until ctx
// ends.
func (m *TalkManager) Run(ctx context.Context) {
	m.sweepStale(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-m.wake:
			for m.applyNext() {
			}
		}
	}
}

// OnPresence is the hub's Presence hook. It must not block, so the event is
// queued (without a bound: only events of open temporary Talks are kept, and
// joining them is rate limited) and applied by Run.
func (m *TalkManager) OnPresence(user, channel uuid.UUID, joined bool) {
	m.mu.Lock()
	if _, ours := m.talks[channel]; !ours {
		m.mu.Unlock()
		return
	}
	m.pending = append(m.pending, presenceEvent{user, channel, joined})
	m.mu.Unlock()
	select {
	case m.wake <- struct{}{}:
	default: // Run is already woken and drains everything pending
	}
}

// Open creates a temporary Talk from lobbyID for user, who leads it. The
// caller has checked that user may join the lobby; this is the limit and the
// creation. The new Talk is removed again if nobody enters it.
func (m *TalkManager) Open(ctx context.Context, user, lobbyID uuid.UUID) (*ChannelInfo, error) {
	if m.CreateLimit != nil {
		if ok, _ := m.CreateLimit.Allow(user.String()); !ok {
			return nil, httpx.ErrRateLimited()
		}
	}
	ch, err := OpenTempTalk(ctx, m.DB, lobbyID, user)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	t := &liveTalk{leader: &user}
	m.talks[ch.ID] = t
	m.armLocked(ch.ID, t)
	m.mu.Unlock()
	m.structureChanged()
	return &ChannelInfo{ID: ch.ID, Type: ch.Type, UserLimit: ch.UserLimit, SlowMode: ch.SlowModeSeconds}, nil
}

// Leader returns the current leader of a temporary Talk.
func (m *TalkManager) Leader(id uuid.UUID) (uuid.UUID, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t := m.talks[id]; t != nil && t.leader != nil {
		return *t.leader, true
	}
	return uuid.Nil, false
}

// IsLeader reports whether user leads the temporary Talk id.
func (m *TalkManager) IsLeader(id, user uuid.UUID) bool {
	leader, ok := m.Leader(id)
	return ok && leader == user
}

// structureChanged is what a created or removed channel needs: the permission
// state rebuilt and every client told to reload the channel list.
func (m *TalkManager) structureChanged() {
	m.Perms.Changed(nil)
	m.Events.Broadcast(events.ChannelsChanged, nil)
}
