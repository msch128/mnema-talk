package discordimport

import (
	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/perm"
)

// Plan is the exact list of what an import creates. It is computed from the
// template and the current state without writing anything; applying it
// recomputes it and refuses when it no longer matches the hash the
// administrator confirmed.
type Plan struct {
	Code         string `json:"code"`
	TemplateName string `json:"template_name"`
	SourceName   string `json:"source_name"`
	// ServerName is what the third confirmation accepts besides "IMPORT".
	ServerName string         `json:"server_name"`
	Categories []PlanCategory `json:"categories"`
	Channels   []PlanChannel  `json:"channels"`
	// Roles are listed highest first; they are created below every existing role.
	Roles []PlanRole `json:"roles"`
	// MappedRoles are the Discord roles the administrator mapped onto an
	// existing role: nothing is created for them.
	MappedRoles []PlanMappedRole `json:"mapped_roles"`
	// MappableRoles are the existing roles the importer may map a Discord
	// role onto (highest first); the roles a role_map may name.
	MappableRoles []RoleChoice `json:"mappable_roles"`
	Everyone      EveryoneInfo `json:"everyone"`
	// Skipped lists template channels that have no counterpart (threads and the like).
	Skipped []string `json:"skipped"`
	// DroppedBits names every Discord permission dropped from the roles and overwrites.
	DroppedBits          []string `json:"dropped_bits"`
	AdministratorDropped bool     `json:"administrator_dropped"`
	Counts               Counts   `json:"counts"`
	// Hash identifies this exact plan; the apply request must repeat it.
	Hash string `json:"hash"`
}

// Counts are the numbers of objects a plan creates.
type Counts struct {
	Categories int `json:"categories"`
	Channels   int `json:"channels"`
	Roles      int `json:"roles"`
}

// PlanCategory is a category to create.
type PlanCategory struct {
	SourceID   string          `json:"source_id"`
	Name       string          `json:"name" maxLength:"64"`
	Overwrites []PlanOverwrite `json:"overwrites"`
}

// PlanChannel is a channel or Talk to create.
type PlanChannel struct {
	SourceID         string `json:"source_id"`
	CategorySourceID string `json:"category_source_id"`
	// Category is the name of the category it goes into ("" for none).
	Category string `json:"category"`
	Name     string `json:"name" maxLength:"64"`
	// Type is the Mnema type: text, voice (a Talk), stage or forum.
	Type string `json:"type"`
	// SourceType names the Discord kind it came from.
	SourceType      string          `json:"source_type"`
	Topic           string          `json:"topic" maxLength:"255"`
	SlowModeSeconds int             `json:"slow_mode_seconds"`
	UserLimit       int             `json:"user_limit"`
	Private         bool            `json:"private"`
	Overwrites      []PlanOverwrite `json:"overwrites"`
	Notes           []string        `json:"notes"`
}

// PlanOverwrite is a permission overwrite of a new category or channel.
type PlanOverwrite struct {
	// RoleSourceID is the template id of the role; Everyone marks @everyone.
	RoleSourceID string   `json:"role_source_id"`
	RoleName     string   `json:"role_name"`
	Everyone     bool     `json:"everyone"`
	Allow        perm.Set `json:"allow" swaggertype:"integer" minimum:"0"`
	Deny         perm.Set `json:"deny" swaggertype:"integer" minimum:"0"`
	AllowKeys    []string `json:"allow_keys"`
	DenyKeys     []string `json:"deny_keys"`
	// MappedRoleID is the existing role the overwrite is written for when the
	// Discord role was mapped onto one.
	MappedRoleID *uuid.UUID `json:"mapped_role_id,omitempty" binding:"optional" format:"uuid"`
}

// PlanRole is a role to create.
type PlanRole struct {
	SourceID             string   `json:"source_id"`
	Name                 string   `json:"name" maxLength:"32"`
	SourceName           string   `json:"source_name"`
	Color                int      `json:"color" minimum:"0" maximum:"16777215"`
	Hoist                bool     `json:"hoist"`
	Mentionable          bool     `json:"mentionable"`
	Permissions          perm.Set `json:"permissions" swaggertype:"integer" minimum:"0"`
	PermissionKeys       []string `json:"permission_keys"`
	Dropped              []string `json:"dropped"`
	AdministratorDropped bool     `json:"administrator_dropped"`
	// SuggestedRoleID is the existing role with the same name (ignoring case)
	// the administrator may map this role onto; null when there is none.
	SuggestedRoleID *uuid.UUID `json:"suggested_role_id" format:"uuid" extensions:"x-nullable"`
}

// PlanMappedRole is a Discord role mapped onto an existing role.
type PlanMappedRole struct {
	SourceID   string    `json:"source_id"`
	SourceName string    `json:"source_name"`
	RoleID     uuid.UUID `json:"role_id" format:"uuid"`
	RoleName   string    `json:"role_name"`
}

// EveryoneInfo shows what Discord's @everyone held. It is information only:
// Mnema's @everyone is never changed.
type EveryoneInfo struct {
	PermissionKeys []string `json:"permission_keys"`
	Dropped        []string `json:"dropped"`
	// Applied is reserved: @everyone is never changed, so it is always false.
	Applied bool `json:"applied"`
}
