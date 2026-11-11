package wire

import "github.com/msch128/mnema-talk/internal/events"

// Pong answers a client's ping with the number it sent (pong).
type Pong struct {
	T float64 `json:"t"`
}

func (Pong) EventName() string { return events.Pong }

// EmojiChanged says a custom emoji was added, renamed or deleted
// (emoji_changed): clients fetch GET /api/emoji again.
type EmojiChanged struct{ signal }

func (EmojiChanged) EventName() string { return events.EmojiChanged }

// CommunityUpdated carries the community's name and the version of its icon
// ("" when it has none) after the Owner changed one of them (community_updated).
type CommunityUpdated struct {
	CommunityIcon string `json:"community_icon"`
	CommunityName string `json:"community_name"`
}

func (CommunityUpdated) EventName() string { return events.CommunityUpdated }
