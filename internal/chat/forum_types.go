package chat

import (
	"time"

	"github.com/google/uuid"
)

const (
	MaxPostTitleLen     = 100
	MaxPostTags         = 5
	MaxForumTags        = 20
	MaxForumTagNameLen  = 20
	MaxForumTagEmojiLen = 32
)

// ForumTag is an admin-defined tag for labeling forum posts in a forum channel.
type ForumTag struct {
	ID        uuid.UUID `json:"id" format:"uuid"`
	ChannelID uuid.UUID `json:"channel_id" format:"uuid"`
	Name      string    `json:"name" maxLength:"20"`
	Emoji     string    `json:"emoji,omitempty" binding:"optional"`
	// Color is a name from ForumTagColors, or empty for the plain look.
	Color string `json:"color,omitempty" binding:"optional" enums:"gray,red,orange,yellow,green,teal,blue,purple,pink"`
	// Position is the tag's place in the forum's own order (1 is first).
	Position int `json:"position"`
	// Moderated tags are applied and removed only by members with Manage messages.
	Moderated bool      `json:"moderated"`
	CreatedAt time.Time `json:"created_at" format:"date-time"`
}
