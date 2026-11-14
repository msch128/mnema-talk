package core

import (
	"time"

	"github.com/google/uuid"
)

type ChannelType string

const (
	ChannelTypeText  ChannelType = "text"
	ChannelTypeVoice ChannelType = "voice"
	ChannelTypeForum ChannelType = "forum"

	// channelKindDM is the type a direct message conversation has in the
	// channels table (dm.go). It is deliberately not a ChannelType: it is never
	// a channel type to create or one the API lists with the channels.
	ChannelKindDM = "dm"

	MaxChannelNameLen = 64
	MaxTopicLen       = 255
)

type Channel struct {
	ID uuid.UUID `json:"id" format:"uuid"`
	// Number is the channel's short, stable link number (/c/12-general).
	Number     int64       `json:"number"`
	CategoryID *uuid.UUID  `json:"category_id" format:"uuid" extensions:"x-nullable"`
	Name       string      `json:"name" maxLength:"64"`
	Type       ChannelType `json:"type"`
	Topic      string      `json:"topic" maxLength:"255"`
	SortOrder  int         `json:"sort_order"`
	CreatedAt  time.Time   `json:"created_at" format:"date-time"`
	// UserLimit is the most members a voice channel admits at once; 0 = no
	// limit.
	UserLimit int `json:"user_limit" minimum:"0" maximum:"999"`
	// SlowModeSeconds is the least time between two messages of one member
	// in this channel; 0 = off. Members who may manage messages here are
	// exempt.
	SlowModeSeconds int `json:"slow_mode_seconds" minimum:"0" maximum:"86400"`
	// AutoDeleteDays is the effective auto-delete period in days for older
	// clients: whole days rounded down and held within 1 to 90 while it is on
	// (the range they accept), 0 when it is off or deletes only files
	// (auto_delete_scope "attachments"). Never set on direct messages.
	// auto_delete_hours is the precise value (doc/timed-messages.md).
	AutoDeleteDays int `json:"auto_delete_days" minimum:"0" maximum:"90" binding:"optional"`
	// AutoDeleteHours is the effective period after which messages (or, with
	// the scope "attachments", their files) are removed; 0 = off. Pinned
	// messages stay.
	AutoDeleteHours int `json:"auto_delete_hours" minimum:"0" maximum:"8760" binding:"optional"`
	// AutoDeleteScope is what the period removes: "all" the messages with
	// their files, "attachments" only the files.
	AutoDeleteScope string `json:"auto_delete_scope" enums:"all,attachments" binding:"optional"`
	// AutoDeleteFollowsDefault says the channel has no setting of its own and
	// follows the community default (which may be off).
	AutoDeleteFollowsDefault bool `json:"auto_delete_follows_default" binding:"optional"`
	// IsLobby marks a Talk as a "Create a Talk" lobby: joining it opens a
	// temporary Talk instead of taking a seat (doc/talks.md).
	IsLobby bool `json:"is_lobby" binding:"optional"`
	// IsTemp marks a temporary Talk opened from a lobby; it is removed again
	// once it stays empty.
	IsTemp bool `json:"is_temp" binding:"optional"`
	// LeaderID is the leader of a temporary Talk: the member who may rename
	// it and set its member limit. Absent on every other channel.
	LeaderID *uuid.UUID `json:"leader_id,omitempty" binding:"optional" format:"uuid"`
	// IsStage marks a Talk as a Stage: without talk power nobody speaks or
	// shares video there (doc/talks.md).
	IsStage bool `json:"is_stage" binding:"optional"`
	// Slug is the channel's address in the web app (/c/general, /v/lounge):
	// unique server-wide, lowercase ASCII. The id stays the key everywhere.
	Slug string `json:"slug" maxLength:"60" binding:"optional"`
	// PreviousSlugs are the slugs the channel had before a rename, oldest
	// first. Old links resolve through them; no other channel ever takes one.
	PreviousSlugs []string `json:"previous_slugs" binding:"optional"`
}

// PrivateChannelSQL is the SQL condition, for the channels row called alias,
// that holds for a conversation and for the call Talk of one. Both are as
// private as the conversation: hidden from the administrative lists, never
// audited, never swept. A call Talk is a voice channel with a dm_id, not a row
// of type 'dm', so a condition on the type alone forgets it (H-L6-04 to 07):
// use this one wherever a conversation is told from a community channel in SQL.
func PrivateChannelSQL(alias string) string {
	return "(" + alias + ".type = '" + ChannelKindDM + "' OR " + alias + ".dm_id IS NOT NULL)"
}

// Auto-delete scopes: what a channel's period removes.
const (
	AutoDeleteScopeAll         = "all"
	AutoDeleteScopeAttachments = "attachments"
)

// AutoDeleteSQL selects, for the channels row called alias, the effective
// auto-delete hours, scope and whether they come from the community default,
// in that order. A conversation and its call Talk answer 0: never swept.
func AutoDeleteSQL(alias string) string {
	return `CASE WHEN ` + PrivateChannelSQL(alias) + ` THEN 0
	             ELSE COALESCE(` + alias + `.auto_delete_hours, (SELECT auto_delete_default_hours FROM instance_settings)) END,
	        CASE WHEN ` + alias + `.auto_delete_hours IS NULL THEN (SELECT auto_delete_default_scope FROM instance_settings)
	             ELSE ` + alias + `.auto_delete_scope END,
	        ` + alias + `.auto_delete_hours IS NULL AND NOT ` + PrivateChannelSQL(alias)
}

// FillAutoDeleteDays works out the older days field from the effective hours
// and scope (see Channel.AutoDeleteDays).
func (c *Channel) FillAutoDeleteDays() {
	c.AutoDeleteDays = AutoDeleteDays(c.AutoDeleteHours, c.AutoDeleteScope)
}

// maxLegacyAutoDeleteDays is the largest auto_delete_days an older client
// accepts.
const maxLegacyAutoDeleteDays = 90

// AutoDeleteDays is the days field for an effective period and scope.
func AutoDeleteDays(hours int, scope string) int {
	if hours <= 0 || scope == AutoDeleteScopeAttachments {
		return 0
	}
	return min(max(1, hours/24), maxLegacyAutoDeleteDays)
}
