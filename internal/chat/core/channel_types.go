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
	// AutoDeleteDays removes messages older than this many days (1, 7, 30 or
	// 90; pinned messages stay); 0 = off. Never set on direct messages.
	AutoDeleteDays int `json:"auto_delete_days" minimum:"0" maximum:"90" binding:"optional"`
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
