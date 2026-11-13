package mnema

import "time"

// Self is the bot account, the answer of GET /users/@me.
type Self struct {
	ID             string  `json:"id"`
	BotID          string  `json:"bot_id"`
	Username       string  `json:"username"`
	Bot            bool    `json:"bot"`
	AvatarURL      string  `json:"avatar_url,omitempty"`
	AllowedIntents Intents `json:"allowed_intents"`
}

// User is a member as events and interactions show them.
type User struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	AvatarURL   string `json:"avatar_url,omitempty"`
	Bot         bool   `json:"bot,omitempty"`
}

// Member is a member as GET /members lists them.
type Member = User

// ChannelHierarchy is the answer of GET /channels: the channels the bot can
// view, grouped by category.
type ChannelHierarchy struct {
	Categories    []Category `json:"categories"`
	Uncategorized []Channel  `json:"uncategorized"`
}

// All returns every channel of the hierarchy, categorised ones first.
func (h *ChannelHierarchy) All() []Channel {
	var out []Channel
	for _, c := range h.Categories {
		out = append(out, c.Channels...)
	}
	return append(out, h.Uncategorized...)
}

// Category groups channels.
type Category struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	SortOrder int       `json:"sort_order"`
	CreatedAt time.Time `json:"created_at"`
	Channels  []Channel `json:"channels"`
}

// Channel is a channel the bot can view.
type Channel struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Type            string    `json:"type"`
	Topic           string    `json:"topic"`
	CategoryID      *string   `json:"category_id"`
	Number          int       `json:"number"`
	SortOrder       int       `json:"sort_order"`
	SlowModeSeconds int       `json:"slow_mode_seconds"`
	Slug            string    `json:"slug,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
}

// Message is a message as the REST API and the gateway carry it.
type Message struct {
	ID          string       `json:"id"`
	ChannelID   string       `json:"channel_id"`
	UserID      string       `json:"user_id"`
	Username    string       `json:"username"`
	DisplayName string       `json:"display_name"`
	AvatarURL   string       `json:"avatar_url,omitempty"`
	IsBot       bool         `json:"is_bot,omitempty"`
	IsWebhook   bool         `json:"is_webhook,omitempty"`
	Content     string       `json:"content"`
	Embeds      []Embed      `json:"embeds,omitempty"`
	Components  []Component  `json:"components,omitempty"`
	Attachments []Attachment `json:"attachments,omitempty"`
	Reactions   []Reaction   `json:"reactions,omitempty"`
	Mentions    []string     `json:"mentions,omitempty"`
	MentionRole []string     `json:"mention_roles,omitempty"`
	Number      int64        `json:"number"`
	ParentID    string       `json:"parent_id,omitempty"`
	ReplyToID   string       `json:"reply_to_id,omitempty"`
	ReplyCount  int          `json:"reply_count"`
	IsEdited    bool         `json:"is_edited"`
	IsPinned    bool         `json:"is_pinned"`
	Nonce       string       `json:"nonce,omitempty"`
	Flags       int          `json:"flags,omitempty"`
	Forward     *Forward     `json:"forward,omitempty"`
	CreatedAt   time.Time    `json:"created_at"`
	UpdatedAt   time.Time    `json:"updated_at"`
}

// MentionsUser reports whether the message mentions the user with the given id
// (for a bot: Ready.User.ID).
func (m *Message) MentionsUser(id string) bool {
	for _, u := range m.Mentions {
		if u == id {
			return true
		}
	}
	return false
}

// Forward marks a forwarded message; its content is the forwarder's comment.
type Forward struct {
	Forwarded bool `json:"forwarded"`
}

// Embed is a rich embed of a message.
type Embed struct {
	Title       string       `json:"title,omitempty"`
	Description string       `json:"description,omitempty"`
	URL         string       `json:"url,omitempty"`
	Color       int          `json:"color,omitempty"`
	Timestamp   string       `json:"timestamp,omitempty"`
	Author      *EmbedAuthor `json:"author,omitempty"`
	Footer      *EmbedFooter `json:"footer,omitempty"`
	Image       *EmbedMedia  `json:"image,omitempty"`
	Thumbnail   *EmbedMedia  `json:"thumbnail,omitempty"`
	Fields      []EmbedField `json:"fields,omitempty"`
}

// EmbedAuthor is the author line of an embed.
type EmbedAuthor struct {
	Name string `json:"name"`
	URL  string `json:"url,omitempty"`
}

// EmbedFooter is the footer of an embed.
type EmbedFooter struct {
	Text string `json:"text"`
}

// EmbedMedia is the image or thumbnail of an embed.
type EmbedMedia struct {
	URL string `json:"url"`
}

// EmbedField is one name/value field of an embed.
type EmbedField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline,omitempty"`
}

// Attachment is a file of a message.
type Attachment struct {
	ID               string `json:"id"`
	URL              string `json:"url"`
	OriginalFilename string `json:"original_filename"`
	MimeType         string `json:"mime_type"`
	SizeBytes        int64  `json:"size_bytes"`
	IsDeleted        bool   `json:"is_deleted"`
}

// Reaction is the summary of one emoji on a message.
type Reaction struct {
	Emoji string   `json:"emoji"`
	Count int      `json:"count"`
	Users []string `json:"users"`
}

// AllowedMentions limits whom a message notifies: Parse may contain
// "everyone" and "roles"; Users and Roles list ids.
type AllowedMentions struct {
	Parse []string `json:"parse,omitempty"`
	Users []string `json:"users,omitempty"`
	Roles []string `json:"roles,omitempty"`
}

// MessageReference makes a message a reply to another one.
type MessageReference struct {
	MessageID string `json:"message_id"`
}

// Ready is the READY dispatch: the session started.
type Ready struct {
	V                 int     `json:"v"`
	User              Self    `json:"user"`
	SessionID         string  `json:"session_id"`
	Intents           Intents `json:"intents"`
	ResumeGatewayURL  string  `json:"resume_gateway_url"`
	HeartbeatInterval int     `json:"heartbeat_interval"`
}

// ChannelsUpdate is the CHANNELS_UPDATE dispatch: the channel layout or the
// bot's access changed; fetch REST.Channels again.
type ChannelsUpdate struct{}

// MemberAdd is the GUILD_MEMBER_ADD dispatch.
type MemberAdd = User

// MessageDelete is the MESSAGE_DELETE dispatch.
type MessageDelete struct {
	ID        string `json:"id"`
	ChannelID string `json:"channel_id"`
	ParentID  string `json:"parent_id,omitempty"`
}

// ReactionsUpdate is the MESSAGE_REACTIONS_UPDATE dispatch: the new summary.
type ReactionsUpdate struct {
	MessageID string     `json:"message_id"`
	ChannelID string     `json:"channel_id"`
	Reactions []Reaction `json:"reactions"`
}

// Intents choose the event classes of a gateway session (Discord's bits).
type Intents int64

// The intents the gateway knows.
const (
	IntentGuilds                Intents = 1 << 0
	IntentGuildMembers          Intents = 1 << 1
	IntentGuildMessages         Intents = 1 << 9
	IntentGuildMessageReactions Intents = 1 << 10
)

// Names returns the manifest names of the intents ("GUILDS", ...).
func (i Intents) Names() []string {
	all := []struct {
		bit  Intents
		name string
	}{{IntentGuilds, "GUILDS"}, {IntentGuildMembers, "GUILD_MEMBERS"}, {IntentGuildMessages, "GUILD_MESSAGES"}, {IntentGuildMessageReactions, "GUILD_MESSAGE_REACTIONS"}}
	var out []string
	for _, x := range all {
		if i&x.bit != 0 {
			out = append(out, x.name)
		}
	}
	return out
}
