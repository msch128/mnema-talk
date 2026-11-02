package communityevents

import (
	"time"

	"github.com/google/uuid"
)

const (
	// MaxCommentLen is the longest comment in characters.
	MaxCommentLen = 2000
	// MaxEventComments is the most comments one event keeps.
	MaxEventComments = 500
	// CommentsPerMinute bounds how often a member may comment or edit.
	CommentsPerMinute = 10
	// DefaultCommentPage and MaxCommentPage size the comment list.
	DefaultCommentPage = 50
	MaxCommentPage     = 100

	// CommentCreated, CommentUpdated and CommentDeleted are the actions of a comment event.
	CommentCreated = "created"
	CommentUpdated = "updated"
	CommentDeleted = "deleted"
)

// EventComment is one comment under an event.
type EventComment struct {
	ID        uuid.UUID  `json:"id" format:"uuid"`
	EventID   uuid.UUID  `json:"event_id" format:"uuid"`
	User      UserRef    `json:"user"`
	Content   string     `json:"content" maxLength:"2000"`
	CreatedAt time.Time  `json:"created_at" format:"date-time"`
	EditedAt  *time.Time `json:"edited_at,omitempty" format:"date-time" binding:"optional" extensions:"x-nullable"`
}

// CommentRequest is the body of a new or edited comment.
type CommentRequest struct {
	Content string `json:"content" minLength:"1" maxLength:"2000"`
}

// CommentList is a page of comments, oldest first, and whether older ones exist.
type CommentList struct {
	Comments []EventComment `json:"comments"`
	HasMore  bool           `json:"has_more"`
	// Muted says the caller switched the notices of this event's comments off.
	Muted bool `json:"muted"`
}

// CommentMute sets whether the caller is told about new comments.
type CommentMute struct {
	Muted bool `json:"muted"`
}

// CommentPayload is broadcast to everyone who may see the event when a comment changes.
type CommentPayload struct {
	Action    string        `json:"action" enums:"created,updated,deleted"`
	EventID   uuid.UUID     `json:"event_id" format:"uuid"`
	CommentID uuid.UUID     `json:"comment_id" format:"uuid"`
	Comment   *EventComment `json:"comment,omitempty" binding:"optional"`
}

// CommentNoticePayload is sent to the members who are told about a new comment (the event's creator and attendees).
type CommentNoticePayload struct {
	EventID    uuid.UUID  `json:"event_id" format:"uuid"`
	Title      string     `json:"title"`
	AuthorName string     `json:"author_name"`
	Excerpt    string     `json:"excerpt"`
	ChannelID  *uuid.UUID `json:"channel_id,omitempty" format:"uuid" binding:"optional"`
}
