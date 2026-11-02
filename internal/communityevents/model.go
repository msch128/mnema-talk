package communityevents

import (
	"bytes"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

const (
	StatusScheduled = "scheduled"
	StatusActive    = "active"
	StatusCompleted = "completed"
	StatusCancelled = "cancelled"

	RSVPGoing      = "going"
	RSVPInterested = "interested"
	RSVPNotGoing   = "not_going"
)

// RSVPCounts summarizes responses for an event.
type RSVPCounts struct {
	Going      int `json:"going"`
	Interested int `json:"interested"`
	NotGoing   int `json:"not_going"`
}

// UserRef is a minimal representation of an event participant.
type UserRef struct {
	ID          uuid.UUID `json:"id" format:"uuid"`
	Username    string    `json:"username"`
	DisplayName string    `json:"display_name"`
	AvatarURL   *string   `json:"avatar_url,omitempty" binding:"optional" extensions:"x-nullable"`
}

// CommunityEvent is a community event.
type CommunityEvent struct {
	ID           uuid.UUID  `json:"id" format:"uuid"`
	Title        string     `json:"title" maxLength:"100"`
	Description  string     `json:"description" maxLength:"2000"`
	StartTime    time.Time  `json:"start_time" format:"date-time"`
	EndTime      *time.Time `json:"end_time,omitempty" format:"date-time" binding:"optional" extensions:"x-nullable"`
	ChannelID    *uuid.UUID `json:"channel_id,omitempty" format:"uuid" binding:"optional" extensions:"x-nullable"`
	Location     *string    `json:"location,omitempty" maxLength:"255" binding:"optional" extensions:"x-nullable"`
	CoverURL     *string    `json:"cover_url,omitempty" binding:"optional" extensions:"x-nullable"`
	Status       string     `json:"status"`
	CreatedBy    *uuid.UUID `json:"created_by,omitempty" format:"uuid" binding:"optional" extensions:"x-nullable"`
	CreatedAt    time.Time  `json:"created_at" format:"date-time"`
	UpdatedAt    time.Time  `json:"updated_at" format:"date-time"`
	RSVPCounts   RSVPCounts `json:"rsvp_counts"`
	MyRSVP       *string    `json:"my_rsvp,omitempty" binding:"optional" extensions:"x-nullable"`
	GoingMembers []UserRef  `json:"going_members,omitempty" binding:"optional"`
	// Roles are the event's roles with their sign-up counts (in position order).
	Roles []EventRole `json:"roles" binding:"optional"`
	// MySignup is the caller's own sign-up for a role; the answer to a request, never broadcast.
	MySignup *Signup `json:"my_signup,omitempty" binding:"optional"`
	// Signups lists who is signed up for which role (event details only).
	Signups []SignupItem `json:"signups,omitempty" binding:"optional"`
}

// CreateEventRequest is the payload to create a new community event.
type CreateEventRequest struct {
	Title       string     `json:"title" maxLength:"100"`
	Description string     `json:"description" maxLength:"2000"`
	StartTime   time.Time  `json:"start_time" format:"date-time"`
	EndTime     *time.Time `json:"end_time,omitempty" format:"date-time" binding:"optional" extensions:"x-nullable"`
	ChannelID   *uuid.UUID `json:"channel_id,omitempty" format:"uuid" binding:"optional" extensions:"x-nullable"`
	Location    *string    `json:"location,omitempty" maxLength:"255" binding:"optional" extensions:"x-nullable"`
}

// UpdateEventRequest is the payload to update an existing community event.
type UpdateEventRequest struct {
	Title       *string    `json:"title,omitempty" maxLength:"100" binding:"optional"`
	Description *string    `json:"description,omitempty" maxLength:"2000" binding:"optional"`
	StartTime   *time.Time `json:"start_time,omitempty" format:"date-time" binding:"optional"`
	EndTime     *time.Time `json:"end_time,omitempty" format:"date-time" binding:"optional" extensions:"x-nullable"`
	ChannelID   *uuid.UUID `json:"channel_id,omitempty" format:"uuid" binding:"optional" extensions:"x-nullable"`
	Location    *string    `json:"location,omitempty" maxLength:"255" binding:"optional" extensions:"x-nullable"`
	Status      *string    `json:"status,omitempty" binding:"optional"`
	// ClearEndTime is set when the request carried "end_time": null, which
	// removes the end time (an absent end_time leaves it unchanged).
	ClearEndTime bool `json:"-"`
}

// UnmarshalJSON decodes the request strictly (unknown fields are refused, as
// httpx.DecodeJSON does) and records an explicit "end_time": null.
func (r *UpdateEventRequest) UnmarshalJSON(data []byte) error {
	type plain UpdateEventRequest
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode((*plain)(r)); err != nil {
		return err
	}
	var probe struct {
		EndTime json.RawMessage `json:"end_time"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return err
	}
	r.ClearEndTime = string(probe.EndTime) == "null"
	return nil
}

// RSVPRequest sets the caller's RSVP response.
type RSVPRequest struct {
	Response string `json:"response" enums:"going,interested,not_going"`
}

// RSVPItem is one member's RSVP record.
type RSVPItem struct {
	User      UserRef   `json:"user"`
	Response  string    `json:"response"`
	UpdatedAt time.Time `json:"updated_at"`
}

// EventDeletePayload is sent to a member who may no longer see an event.
type EventDeletePayload struct {
	ID uuid.UUID `json:"id" format:"uuid"`
}

// EventReminderPayload is sent over WebSocket when an event is starting in 15 minutes.
type EventReminderPayload struct {
	EventID   uuid.UUID  `json:"event_id"`
	Title     string     `json:"title"`
	StartTime time.Time  `json:"start_time"`
	ChannelID *uuid.UUID `json:"channel_id,omitempty"`
}
