package communityevents

import (
	"time"

	"github.com/google/uuid"
)

const (
	// MaxEventRoles is the most roles one event has.
	MaxEventRoles = 12
	// MaxRoleNameLen is the longest role name in characters.
	MaxRoleNameLen = 40
	// MaxRoleEmojiLen is the longest role emoji in characters.
	MaxRoleEmojiLen = 32
	// MaxRoleCapacity is the largest capacity of a role.
	MaxRoleCapacity = 1000

	SignupConfirmed = "confirmed"
	SignupWaitlist  = "waitlist"
)

// EventRole is one role of an event with how many members hold or wait for it.
type EventRole struct {
	ID        uuid.UUID `json:"id" format:"uuid"`
	Name      string    `json:"name" maxLength:"40"`
	Emoji     string    `json:"emoji,omitempty" maxLength:"32" binding:"optional"`
	Capacity  *int      `json:"capacity,omitempty" minimum:"1" maximum:"1000" binding:"optional" extensions:"x-nullable"`
	Position  int       `json:"position"`
	Confirmed int       `json:"confirmed"`
	Waitlist  int       `json:"waitlist"`
}

// Signup is one member's sign-up for a role.
type Signup struct {
	RoleID uuid.UUID `json:"role_id" format:"uuid"`
	State  string    `json:"state" enums:"confirmed,waitlist"`
	// WaitlistPosition is the place in the role's waitlist (1 is next); 0 when confirmed.
	WaitlistPosition int `json:"waitlist_position"`
}

// SignupItem is a sign-up with the member it belongs to.
type SignupItem struct {
	User      UserRef   `json:"user"`
	RoleID    uuid.UUID `json:"role_id" format:"uuid"`
	State     string    `json:"state" enums:"confirmed,waitlist"`
	CreatedAt time.Time `json:"created_at" format:"date-time"`
}

// RoleInput is a role in a request. A role with an ID keeps its sign-ups.
type RoleInput struct {
	ID       *uuid.UUID `json:"id,omitempty" format:"uuid" binding:"optional" extensions:"x-nullable"`
	Name     string     `json:"name" maxLength:"40"`
	Emoji    string     `json:"emoji,omitempty" maxLength:"32" binding:"optional"`
	Capacity *int       `json:"capacity,omitempty" minimum:"1" maximum:"1000" binding:"optional" extensions:"x-nullable"`
}

// SetRolesRequest replaces the whole role list of an event, in this order.
type SetRolesRequest struct {
	Roles []RoleInput `json:"roles"`
}

// SignupRequest signs the caller up for a role.
type SignupRequest struct {
	RoleID uuid.UUID `json:"role_id" format:"uuid"`
}

// RoleTemplate is a reusable set of roles.
type RoleTemplate struct {
	ID      uuid.UUID   `json:"id" format:"uuid"`
	Name    string      `json:"name" maxLength:"60"`
	Roles   []RoleInput `json:"roles"`
	Enabled bool        `json:"enabled"`
	// Preset marks a template that ships with the server; it can be enabled and disabled, not deleted.
	Preset bool `json:"preset"`
}

// RoleTemplateRequest creates or updates a role template.
type RoleTemplateRequest struct {
	Name    *string      `json:"name,omitempty" maxLength:"60" binding:"optional"`
	Roles   *[]RoleInput `json:"roles,omitempty" binding:"optional"`
	Enabled *bool        `json:"enabled,omitempty" binding:"optional"`
}

// SignupNoticePayload is sent over WebSocket to a member whose sign-up changed without their doing: they moved up from the waitlist a moderator removed them, or their role was removed and they landed on the waitlist of another.
type SignupNoticePayload struct {
	EventID   uuid.UUID  `json:"event_id" format:"uuid"`
	Title     string     `json:"title"`
	Kind      string     `json:"kind" enums:"promoted,removed,waitlisted"`
	RoleName  string     `json:"role_name"`
	StartTime time.Time  `json:"start_time" format:"date-time"`
	ChannelID *uuid.UUID `json:"channel_id,omitempty" format:"uuid" binding:"optional"`
}
