package chat

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/emojitext"
	"github.com/msch128/mnema-talk/internal/httpx"
)

const (
	MaxMessageLen = 4000
	maxEmojiBytes = 32
)

type MediaAttachment struct {
	ID               uuid.UUID `json:"id" format:"uuid"`
	OriginalFilename string    `json:"original_filename"`
	MimeType         string    `json:"mime_type"`
	SizeBytes        int64     `json:"size_bytes"`
	// URL is /api/media/{id}.
	URL       string `json:"url"`
	IsDeleted bool   `json:"is_deleted"`
}

type ReactionSummary struct {
	Emoji string      `json:"emoji"`
	Count int         `json:"count"`
	Users []uuid.UUID `json:"users" format:"uuid"`
}

type Message struct {
	ID uuid.UUID `json:"id" format:"uuid"`
	// Number is the message's short, stable link number (/m/4821).
	Number      int64             `json:"number"`
	ChannelID   uuid.UUID         `json:"channel_id" format:"uuid"`
	UserID      uuid.UUID         `json:"user_id" format:"uuid"`
	ParentID    *uuid.UUID        `json:"parent_id,omitempty" binding:"optional" format:"uuid"`
	Username    string            `json:"username"`
	DisplayName string            `json:"display_name"`
	AvatarURL   string            `json:"avatar_url,omitempty" binding:"optional"`
	Content     string            `json:"content" maxLength:"4000"`
	IsPinned    bool              `json:"is_pinned"`
	IsEdited    bool              `json:"is_edited"`
	ReplyCount  int               `json:"reply_count"`
	ReplyToID   *uuid.UUID        `json:"reply_to_id,omitempty" binding:"optional" format:"uuid"`
	ReplyTo     *ReplyPreview     `json:"reply_to,omitempty" binding:"optional"`
	Attachments []MediaAttachment `json:"attachments"`
	Reactions   []ReactionSummary `json:"reactions"`
	// Mentions are the users this message mentions (@username, @all, @here).
	Mentions []uuid.UUID `json:"mentions" format:"uuid"`
	// MentionRoles are the roles this message mentions (@Role); the client
	// draws them as role pills. A role that was deleted since drops out. The
	// server always sends it; it is optional for clients that build messages
	// themselves.
	MentionRoles []uuid.UUID `json:"mention_roles" binding:"optional" format:"uuid"`
	CreatedAt    time.Time   `json:"created_at" format:"date-time"`
	UpdatedAt    time.Time   `json:"updated_at" format:"date-time"`
	// IsBot is true for messages posted by a webhook. Their author is a
	// hidden bot account; DisplayName is the webhook's name or the username
	// the sender gave for this message.
	IsBot bool `json:"is_bot,omitempty" binding:"optional"`
	// IsWebhook narrows IsBot: the author is an incoming webhook (shown with
	// a Webhook label), not a bot account (shown with a BOT label).
	IsWebhook bool `json:"is_webhook,omitempty" binding:"optional"`
	// Embeds are the rich cards of a webhook message (Discord-style subset).
	Embeds []Embed `json:"embeds,omitempty" binding:"optional"`
	// Poll is set when the message is a poll; its content is the question.
	Poll *Poll `json:"poll,omitempty" binding:"optional"`
	// PinnedAt and PinnedBy say when and by whom a pinned message was pinned;
	// both are absent on an unpinned message (PinnedBy also when the member
	// is gone).
	PinnedAt *time.Time `json:"pinned_at,omitempty" binding:"optional" format:"date-time"`
	PinnedBy *UserRef   `json:"pinned_by,omitempty" binding:"optional"`
	// Forward is set when the message forwards another one. It says so and
	// nothing more: no id and no text of the original travels with the
	// message, because readers differ in what they may see. Each reader gets
	// the original (with its ids) from GET /api/messages/{id}/forward.
	Forward *ForwardRef `json:"forward,omitempty" binding:"optional"`
	// Title is the required title of a root post in a forum channel (max 100 characters).
	Title *string `json:"title,omitempty" binding:"optional" maxLength:"100"`
	// IsLocked marks a forum post as locked: only moderators can post replies.
	IsLocked bool `json:"is_locked,omitempty" binding:"optional"`
	// Tags are the forum tags attached to a forum post (up to 5 tags).
	Tags []ForumTag `json:"tags,omitempty" binding:"optional"`
	// LastActivityAt is the timestamp of the latest reply in the thread, or the post's creation time.
	LastActivityAt *time.Time `json:"last_activity_at,omitempty" binding:"optional" format:"date-time"`
}

// ForwardRef marks a forwarded message.
type ForwardRef struct {
	Forwarded bool `json:"forwarded"`
}

// UserRef is a member in short: enough to show a name and an avatar.
type UserRef struct {
	ID          uuid.UUID `json:"id" format:"uuid"`
	Username    string    `json:"username"`
	DisplayName string    `json:"display_name"`
	AvatarURL   string    `json:"avatar_url,omitempty" binding:"optional"`
}

// Embed is a rich card on a webhook message: the Discord embed fields Mnema
// Talk renders. Images are shown as links, never loaded from their host.
type Embed struct {
	Title       string       `json:"title,omitempty" binding:"optional" maxLength:"256"`
	Description string       `json:"description,omitempty" binding:"optional" maxLength:"4096"`
	URL         string       `json:"url,omitempty" binding:"optional" maxLength:"2048"`
	Color       *int         `json:"color,omitempty" binding:"optional" minimum:"0" maximum:"16777215"`
	Timestamp   string       `json:"timestamp,omitempty" binding:"optional"`
	Footer      *EmbedFooter `json:"footer,omitempty" binding:"optional"`
	Image       *EmbedMedia  `json:"image,omitempty" binding:"optional"`
	Thumbnail   *EmbedMedia  `json:"thumbnail,omitempty" binding:"optional"`
	Author      *EmbedAuthor `json:"author,omitempty" binding:"optional"`
	Fields      []EmbedField `json:"fields,omitempty" binding:"optional"`
	// TalkID turns the embed into an invitation to this Talk: the client
	// offers a Join button. Only the server sets it (a Talk invitation,
	// talks_invite.go); webhook payloads cannot. Joining goes through the
	// ordinary permission checks, the card grants nothing.
	TalkID *uuid.UUID `json:"talk_id,omitempty" binding:"optional" format:"uuid"`
}

// EmbedFooter is the small text at the bottom of an embed.
type EmbedFooter struct {
	Text string `json:"text" maxLength:"2048"`
}

// EmbedMedia is the link of an embed's image or thumbnail.
type EmbedMedia struct {
	URL string `json:"url" maxLength:"2048"`
}

// EmbedAuthor is the line above an embed's title.
type EmbedAuthor struct {
	Name string `json:"name" maxLength:"256"`
	URL  string `json:"url,omitempty" binding:"optional" maxLength:"2048"`
}

// EmbedField is one name/value pair of an embed.
type EmbedField struct {
	Name   string `json:"name" maxLength:"256"`
	Value  string `json:"value" maxLength:"1024"`
	Inline bool   `json:"inline,omitempty" binding:"optional"`
}

// ReplyPreview is the quoted header of a reply. Deleted is true when the
// original message no longer exists.
type ReplyPreview struct {
	ID             uuid.UUID `json:"id" format:"uuid"`
	Deleted        bool      `json:"deleted"`
	UserID         uuid.UUID `json:"user_id,omitempty" binding:"optional" format:"uuid"`
	Username       string    `json:"username,omitempty" binding:"optional"`
	DisplayName    string    `json:"display_name,omitempty" binding:"optional"`
	AvatarURL      string    `json:"avatar_url,omitempty" binding:"optional"`
	Content        string    `json:"content,omitempty" binding:"optional"`
	HasAttachments bool      `json:"has_attachments,omitempty" binding:"optional"`
	// IsBot and IsWebhook describe the quoted author like Message does.
	IsBot     bool `json:"is_bot,omitempty" binding:"optional"`
	IsWebhook bool `json:"is_webhook,omitempty" binding:"optional"`
}

// ValidateContent trims and length-checks message text. Empty content is only
// allowed when the message carries an attachment.
func ValidateContent(content string, allowEmpty bool) (string, error) {
	return httpx.CleanText("content", content, MaxMessageLen, !allowEmpty)
}

// Parts of emoji sequences that are format characters: the zero width joiner
// of family and profession emoji, and the tag characters of subdivision flags.
const (
	zeroWidthJoiner rune = 0x200D
	tagSpace        rune = 0xE0020
	tagCancel       rune = 0xE007F
)

func isDisallowedEmojiRune(r rune) bool {
	switch {
	case unicode.IsSpace(r), unicode.IsControl(r):
		return true
	case r == '<', r == '>':
		return true
	case unicode.Is(unicode.Cf, r):
		// Invisible and bidi formatting characters spoof the pill; only the
		// joiner and the tag characters of emoji sequences are real emoji parts.
		return r != zeroWidthJoiner && (r < tagSpace || r > tagCancel)
	default:
		return false
	}
}

func validEmojiLength(emoji string) bool {
	if emoji == "" || len(emoji) > maxEmojiBytes {
		return false
	}
	return utf8.ValidString(emoji)
}

// ValidateEmoji accepts a short, printable, whitespace-free reaction token
// without invisible or bidi formatting characters, or the token of a custom emoji (<:name:id>).
func ValidateEmoji(emoji string) (string, error) {
	emoji = strings.TrimSpace(emoji)
	if emojitext.IsToken(emoji) {
		return emoji, nil
	}
	if !validEmojiLength(emoji) {
		return "", httpx.ErrInvalidInput("invalid emoji")
	}
	for _, r := range emoji {
		if isDisallowedEmojiRune(r) {
			return "", httpx.ErrInvalidInput("invalid emoji")
		}
	}
	return emoji, nil
}

// HistoryQuery selects a page of a channel's root messages. At most one anchor
// is set: Before/After page relative to a message (exclusive), Around returns
// the anchor with messages on both sides (for jump-to-message). No anchor
// returns the newest page.
type HistoryQuery struct {
	Limit  int
	Before *uuid.UUID
	After  *uuid.UUID
	Around *uuid.UUID
	// BeforeActivity is the latest activity of the Before post as the client
	// saw it; only the forum post list pages by activity and uses it.
	BeforeActivity *time.Time
}
