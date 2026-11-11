package wire

import (
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/events"
)

// moderation_notice tells the member a moderator, or auto-moderation, acted on
// what happened. It has four shapes; kind tells them apart.

// ModerationNotice is the notice of a timeout, its removal or a ban (kind
// "timeout", "timeout_removed", "ban"); until and duration_seconds are left
// out when they do not apply.
type ModerationNotice struct {
	Kind    string     `json:"kind" enums:"timeout,timeout_removed,ban"`
	Until   *time.Time `json:"until,omitempty" format:"date-time"`
	Seconds int        `json:"duration_seconds,omitempty"`
	Reason  string     `json:"reason"`
}

func (ModerationNotice) EventName() string { return events.ModerationNotice }

// AutomodTimeoutNotice is the notice of a timeout that auto-moderation or raid
// protection gave (kind is always "timeout"); duration_seconds is the time left.
type AutomodTimeoutNotice struct {
	DurationSeconds int       `json:"duration_seconds"`
	Kind            string    `json:"kind" enums:"timeout"`
	Reason          string    `json:"reason"`
	Until           time.Time `json:"until" format:"date-time"`
}

func (AutomodTimeoutNotice) EventName() string { return events.ModerationNotice }

// MessageDeletedNotice tells an author that a moderator removed their message
// (kind is always "message_deleted").
type MessageDeletedNotice struct {
	ChannelName string `json:"channel_name"`
	Kind        string `json:"kind" enums:"message_deleted"`
	Reason      string `json:"reason"`
}

func (MessageDeletedNotice) EventName() string { return events.ModerationNotice }

// MessagesDeletedNotice tells an author that a moderator removed count of
// their latest messages at once (kind is always "messages_deleted").
type MessagesDeletedNotice struct {
	ChannelName string `json:"channel_name"`
	Count       int    `json:"count"`
	Kind        string `json:"kind" enums:"messages_deleted"`
	Reason      string `json:"reason"`
}

func (MessagesDeletedNotice) EventName() string { return events.ModerationNotice }

// MemberTimeout says a member's timeout started (timeout_until) or ended
// (null) (member_timeout).
type MemberTimeout struct {
	ID           uuid.UUID  `json:"id" format:"uuid"`
	TimeoutUntil *time.Time `json:"timeout_until" format:"date-time"`
}

func (MemberTimeout) EventName() string { return events.MemberTimeout }

// Kicked tells a member that a moderator signed them out everywhere (kicked).
type Kicked struct {
	Reason string `json:"reason"`
}

func (Kicked) EventName() string { return events.Kicked }

// RolesChanged says a role was created, edited, deleted, reordered or
// assigned (roles_changed): clients fetch the roles and members again.
type RolesChanged struct{ signal }

func (RolesChanged) EventName() string { return events.RolesChanged }

// PermissionsChanged tells one connection that what its member may do changed
// (permissions_changed): it fetches its permissions and the channels again.
type PermissionsChanged struct{ signal }

func (PermissionsChanged) EventName() string { return events.PermissionsChanged }

// MemberRolesChanged carries the roles a member holds after taking or
// dropping one they may choose themselves (member_roles_changed).
type MemberRolesChanged struct {
	ID      uuid.UUID   `json:"id" format:"uuid"`
	RoleIDs []uuid.UUID `json:"role_ids" format:"uuid"`
}

func (MemberRolesChanged) EventName() string { return events.MemberRolesChanged }

// ReportsOpen carries how many reports are open now (reports_open); it goes
// only to members who hold Manage reports.
type ReportsOpen struct {
	Open int `json:"open"`
}

func (ReportsOpen) EventName() string { return events.ReportsOpen }

// SystemUpdate says a newer release is available (system_update).
type SystemUpdate struct {
	Version string `json:"version"`
}

func (SystemUpdate) EventName() string { return events.SystemUpdate }
