package wire

import (
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/events"
)

// MessageDelete says a message is gone (message_delete); parent_id is the
// thread it was a reply in, null for a message of the channel itself.
type MessageDelete struct {
	ChannelID uuid.UUID  `json:"channel_id" format:"uuid"`
	ID        uuid.UUID  `json:"id" format:"uuid"`
	ParentID  *uuid.UUID `json:"parent_id" format:"uuid"`
}

func (MessageDelete) EventName() string { return events.MessageDelete }

// PinRemoved is the pins_changed of an account erasure: the message is no
// longer pinned, and the actor, the deleted account, has no name. A pin or
// unpin by a member is messages.PinsChanged.
type PinRemoved struct {
	ActorID   uuid.UUID `json:"actor_id" format:"uuid"`
	ActorName string    `json:"actor_name"`
	ChannelID uuid.UUID `json:"channel_id" format:"uuid"`
	MessageID uuid.UUID `json:"message_id" format:"uuid"`
	Pinned    bool      `json:"pinned"`
}

func (PinRemoved) EventName() string { return events.PinsChanged }

// ThreadFollowUpdated tells a member's own sessions that they follow, or no
// longer follow, a thread (thread_follow_updated).
type ThreadFollowUpdated struct {
	ChannelID uuid.UUID `json:"channel_id" format:"uuid"`
	Following bool      `json:"following"`
	ThreadID  uuid.UUID `json:"thread_id" format:"uuid"`
}

func (ThreadFollowUpdated) EventName() string { return events.ThreadFollowUpdated }

// Typing says a member is writing in a channel (typing).
type Typing struct {
	ChannelID uuid.UUID `json:"channel_id" format:"uuid"`
	UserID    uuid.UUID `json:"user_id" format:"uuid"`
}

func (Typing) EventName() string { return events.Typing }

// read_state goes to a member's own sessions and has four shapes.

// ReadStateMarked says the member read a channel up to last_read_at.
type ReadStateMarked struct {
	ChannelID    uuid.UUID `json:"channel_id" format:"uuid"`
	LastReadAt   time.Time `json:"last_read_at" format:"date-time"`
	MentionCount int       `json:"mention_count"`
	UnreadCount  int       `json:"unread_count"`
}

func (ReadStateMarked) EventName() string { return events.ReadState }

// ReadStateUnread says the member marked a channel unread from last_read_at
// on: the sessions fetch the counts again (refresh is always true).
type ReadStateUnread struct {
	ChannelID  uuid.UUID `json:"channel_id" format:"uuid"`
	LastReadAt time.Time `json:"last_read_at" format:"date-time"`
	Refresh    bool      `json:"refresh" enums:"true"`
}

func (ReadStateUnread) EventName() string { return events.ReadState }

// ReadStateCounts gives the member's counts of one channel after a message
// there was deleted or edited: the counts follow the messages that exist now
// and their current mentions, and neither change brings a message a client
// could count. last_read_at is null before the member's first read.
// last_message_number is the number of the channel's newest message the
// counts include, as in GET /api/read-state: a client counts a message_create
// numbered above it on top, and none at or below it again.
type ReadStateCounts struct {
	ChannelID         uuid.UUID  `json:"channel_id" format:"uuid"`
	LastMessageNumber int64      `json:"last_message_number"`
	LastReadAt        *time.Time `json:"last_read_at" format:"date-time" extensions:"x-nullable"`
	MentionCount      int        `json:"mention_count"`
	UnreadCount       int        `json:"unread_count"`
}

func (ReadStateCounts) EventName() string { return events.ReadState }

// ReadStateChannelRefresh asks the sessions to fetch one channel's counts
// again (refresh is always true).
type ReadStateChannelRefresh struct {
	ChannelID uuid.UUID `json:"channel_id" format:"uuid"`
	Refresh   bool      `json:"refresh" enums:"true"`
}

func (ReadStateChannelRefresh) EventName() string { return events.ReadState }

// ReadStateRefresh asks the sessions to fetch every read state and
// notification setting again (refresh is always true).
type ReadStateRefresh struct {
	Refresh bool `json:"refresh" enums:"true"`
}

func (ReadStateRefresh) EventName() string { return events.ReadState }
