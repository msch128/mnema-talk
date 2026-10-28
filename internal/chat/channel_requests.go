package chat

import "github.com/google/uuid"

// Request and response bodies of the channel and category routes in handler_channels.go.

// ChannelHierarchy is the response of GET /api/channels.
type ChannelHierarchy struct {
	Categories    []Category `json:"categories"`
	Uncategorized []Channel  `json:"uncategorized"`
}

// CreateCategoryRequest is the body of POST /api/admin/categories.
type CreateCategoryRequest struct {
	Name      string `json:"name" minLength:"1" maxLength:"64"`
	SortOrder int    `json:"sort_order" minimum:"-1073741824" maximum:"1073741824" binding:"optional"`
}

// RenameCategoryRequest is the body of PATCH /api/admin/categories/{id}.
type RenameCategoryRequest struct {
	Name string `json:"name" minLength:"1" maxLength:"64"`
}

// RenamedCategory is the response of PATCH /api/admin/categories/{id}.
type RenamedCategory struct {
	ID   uuid.UUID `json:"id" format:"uuid"`
	Name string    `json:"name"`
}

// CreateChannelRequest is the body of POST /api/admin/channels.
type CreateChannelRequest struct {
	CategoryID *uuid.UUID  `json:"category_id" format:"uuid" binding:"optional" extensions:"x-nullable"`
	Name       string      `json:"name" minLength:"1" maxLength:"64"`
	Type       ChannelType `json:"type" default:"text" binding:"optional"`
	Topic      string      `json:"topic" maxLength:"255" binding:"optional"`
	SortOrder  int         `json:"sort_order" minimum:"-1073741824" maximum:"1073741824" binding:"optional"`
	// IsStage creates the Talk as a Stage (voice channels only).
	IsStage bool `json:"is_stage" binding:"optional"`
}

// UpdateChannelRequest is the body of PATCH /api/admin/channels/{id}; omitted
// fields are kept.
type UpdateChannelRequest struct {
	Name  *string `json:"name" minLength:"1" maxLength:"64" binding:"optional"`
	Topic *string `json:"topic" maxLength:"255" binding:"optional"`
	// UserLimit caps a voice channel's members at once; 0 removes the limit.
	UserLimit *int `json:"user_limit" minimum:"0" maximum:"999" binding:"optional"`
	// SlowModeSeconds sets the least time between two messages of one member
	// in the channel (0 turns slow mode off). Members who may manage messages
	// in the channel are exempt.
	SlowModeSeconds *int `json:"slow_mode_seconds" minimum:"0" maximum:"86400" binding:"optional"`
	// IsLobby marks a voice channel as a Create-a-Talk lobby (true) or clears
	// the mark (false): joining a lobby opens a temporary Talk (doc/talks.md).
	IsLobby *bool `json:"is_lobby" binding:"optional"`
	// IsStage turns a permanent Talk into a Stage (true) or back into a normal
	// Talk (false); members without talk power lose their microphone, camera
	// and screen at once (doc/talks.md).
	IsStage *bool `json:"is_stage" binding:"optional"`
}

// LayoutRequest is the body of PUT /api/admin/layout: at most 500 entries in
// total; an unknown ID rejects the whole change.
type LayoutRequest struct {
	Categories []CategoryOrder    `json:"categories" binding:"optional"`
	Channels   []ChannelPlacement `json:"channels" binding:"optional"`
}
