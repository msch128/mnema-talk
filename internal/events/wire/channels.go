package wire

import (
	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/events"
)

// signal is the payload of an event that only says "fetch this again": null.
type signal struct{}

func (signal) MarshalJSON() ([]byte, error) { return []byte("null"), nil }

// ChannelsChanged says the channel layout, or what someone may see of it,
// changed (channels_changed): clients fetch GET /api/channels again.
type ChannelsChanged struct{ signal }

func (ChannelsChanged) EventName() string { return events.ChannelsChanged }

// ForumTagsChanged says the tags of a forum channel were created, edited or
// removed (forum_tags_changed).
type ForumTagsChanged struct {
	ChannelID uuid.UUID `json:"channel_id" format:"uuid"`
}

func (ForumTagsChanged) EventName() string { return events.ForumTagsChanged }
