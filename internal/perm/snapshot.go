package perm

import (
	"net/http"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// Subject is who a permission question is about. Owner comes from the
// account's role ('admin'); the snapshot also knows the owners it loaded, so
// a Subject built from just an ID is enough for people other than the caller.
type Subject struct {
	ID    uuid.UUID
	Owner bool
}

// Role is one role as stored. @everyone (Everyone) is implicit: every member
// has it, it sits at position 0 and cannot be renamed, moved or deleted.
type Role struct {
	ID          uuid.UUID `json:"id" format:"uuid"`
	Name        string    `json:"name" minLength:"1" maxLength:"32"`
	Color       int       `json:"color" minimum:"0" maximum:"16777215"`
	Position    int       `json:"position" minimum:"0"`
	Permissions Set       `json:"permissions" swaggertype:"integer" minimum:"0"`
	Hoist       bool      `json:"hoist"`
	Mentionable bool      `json:"mentionable"`
	Everyone    bool      `json:"is_everyone"`
	// Managed marks a role the server maintains: the shared Bot role (every
	// bot holds it) and each bot's own role (BotUserID). Only bots hold them;
	// they cannot be renamed by hand, removed from a bot or deleted.
	Managed bool `json:"managed,omitempty" binding:"optional"`
	// BotUserID is set on a bot's own managed role: the account of the one bot
	// that holds it. The role is named after the bot and goes with it.
	BotUserID *uuid.UUID `json:"bot_user_id,omitempty" binding:"optional" format:"uuid"`
	// MaxUploadMB overrides the upload limit of this role's members; null
	// inherits the global limit. A member gets the highest override of their
	// roles (@everyone included), never above the server's ceiling.
	MaxUploadMB *int `json:"max_upload_mb" minimum:"1" extensions:"x-nullable"`
	// SelfAssignable lets every member add or remove this role on their own
	// account. Only roles without Administrator or admin-area permissions can be
	// flagged, never @everyone or a managed role. Omitted when false.
	SelfAssignable bool `json:"self_assignable,omitempty" binding:"optional"`
	// Description says in a line what the role is for; the member picker shows it.
	Description string `json:"description,omitempty" maxLength:"200" binding:"optional"`
}

type overwrite struct{ allow, deny Set }

// scope is the overwrites of one category or channel.
type scope struct {
	roles   map[uuid.UUID]overwrite
	members map[uuid.UUID]overwrite
}

// Snapshot is the complete permission state at one moment: roles, role
// assignments, channels with their categories and all overwrites. A single
// community is small, so keeping all of it in memory makes every check a few
// map lookups and lets the WebSocket hub filter events without touching the
// database. It is immutable once loaded; changes replace it.
//
// Resolution (Discord's order):
//
//  1. The Owner has everything, always.
//  2. base = @everyone | the union of the member's roles.
//  3. Administrator in base short-circuits to everything.
//  4. The category's overwrites, then the channel's, each applied as:
//     @everyone, then all of the member's roles combined (deny, then allow),
//     then the member's own (deny, then allow). The channel therefore wins
//     over its category.
//  5. Without ViewChannel nothing channel-scoped remains.
type Snapshot struct {
	loaded     time.Time
	everyoneID uuid.UUID
	everyone   Set
	roles      map[uuid.UUID]Role
	userRoles  map[uuid.UUID][]uuid.UUID
	owners     map[uuid.UUID]bool
	// bots are bot accounts: they do not inherit @everyone.
	bots       map[uuid.UUID]bool
	channelCat map[uuid.UUID]uuid.UUID // uuid.Nil: uncategorized
	// stages are the Talks that are Stages (doc/talks.md).
	stages map[uuid.UUID]bool
	// dms are the direct message conversations with their members (see
	// snapshot_dm.go); dmEnabled is the community's switch for them.
	dms map[uuid.UUID]map[uuid.UUID]bool
	// dmTalks maps a Talk started from a conversation to that conversation:
	// only its members may use it, whoever else holds which permission.
	dmTalks    map[uuid.UUID]uuid.UUID
	dmEnabled  bool
	categories map[uuid.UUID]bool
	channelOv  map[uuid.UUID]*scope
	categoryOv map[uuid.UUID]*scope
	// globalUploadMB is the Owner's global upload limit; nil = the ceiling.
	globalUploadMB *int
	// timeouts are the running timeouts; an entry whose time has passed no
	// longer counts, so expiry needs no reload.
	timeouts map[uuid.UUID]Timeout
	// rulesVersion is the version of the rules members must have accepted;
	// 0 while no rules text is set (nothing is enforced). rulesAccepted holds
	// the version each member accepted (only those above 0), so a member the
	// snapshot has not seen yet counts as pending: it fails closed. Like a
	// timeout, pending rules take TimeoutMask, so REST, the hub and the SFU all
	// enforce it from this one snapshot. The Owner and bots are exempt.
	rulesVersion  int
	rulesAccepted map[uuid.UUID]int
}

// Timeout is a member's timeout: they keep reading but lose TimeoutMask.
type Timeout struct {
	Until  time.Time
	Reason string
}

// Age is how long ago the snapshot was loaded.
func (s *Snapshot) age(now time.Time) time.Duration { return now.Sub(s.loaded) }

// IsBot reports whether the account is a bot.
func (s *Snapshot) IsBot(id uuid.UUID) bool { return s.bots[id] }

// IsOwner reports whether sub is the Owner.
func (s *Snapshot) IsOwner(sub Subject) bool { return sub.Owner || s.owners[sub.ID] }

// RoleIDs lists the custom roles of a member, highest first.
func (s *Snapshot) RoleIDs(userID uuid.UUID) []uuid.UUID {
	return slices.Clone(s.userRoles[userID])
}

// Roles lists every role, highest first; @everyone is last.
func (s *Snapshot) Roles() []Role {
	out := make([]Role, 0, len(s.roles))
	for _, r := range s.roles {
		out = append(out, r)
	}
	slices.SortFunc(out, func(a, b Role) int { return b.Position - a.Position })
	return out
}

// Role returns one role.
func (s *Snapshot) Role(id uuid.UUID) (Role, bool) {
	r, ok := s.roles[id]
	return r, ok
}

// Global is what sub may do server-wide: the Owner everything, otherwise
// @everyone plus the member's roles, or everything for an Administrator.
func (s *Snapshot) Global(sub Subject) Set {
	if s.IsOwner(sub) {
		return All
	}
	p := s.everyone
	if s.bots[sub.ID] {
		// A bot gets nothing it was not given: no @everyone.
		p = 0
	}
	for _, id := range s.userRoles[sub.ID] {
		p |= s.roles[id].Permissions
	}
	if p.Has(Administrator) {
		p = All
	}
	return s.silence(sub.ID, p&All)
}

// Timeout returns the member's running timeout.
func (s *Snapshot) Timeout(userID uuid.UUID) (Timeout, bool) {
	t, ok := s.timeouts[userID]
	if !ok || !t.Until.After(clock()) {
		return Timeout{}, false
	}
	return t, true
}

// Timeouts lists the running timeouts (member to end time).
func (s *Snapshot) Timeouts() map[uuid.UUID]time.Time {
	out := map[uuid.UUID]time.Time{}
	for id := range s.timeouts {
		if t, ok := s.Timeout(id); ok {
			out[id] = t.Until
		}
	}
	return out
}

// silence removes TimeoutMask from p while the member is timed out.
func (s *Snapshot) silence(userID uuid.UUID, p Set) Set {
	if _, ok := s.Timeout(userID); ok || s.RulesPending(userID) {
		return p &^ TimeoutMask
	}
	return p
}

// RulesPending reports whether the member still has to accept the community's
// rules before they may post, react, upload or join a Talk. The Owner and
// bots never do.
func (s *Snapshot) RulesPending(userID uuid.UUID) bool {
	return s.rulesVersion > 0 && s.rulesAccepted[userID] < s.rulesVersion && !s.owners[userID] && !s.bots[userID]
}

// ErrRulesNotAccepted is what an action refused for unaccepted rules answers.
var ErrRulesNotAccepted = httpx.NewAPIError(http.StatusForbidden, httpx.CodeRulesNotAccepted, "accept the community rules first")

// Deny is CheckChannel for the member sub: the same refusal, except that a
// permission the pending rules took away answers RULES_NOT_ACCEPTED, so the
// client can show the rules instead of a bare "missing permission".
func (s *Snapshot) Deny(sub Subject, p, need Set) error {
	err := CheckChannel(p, need)
	if err != nil && p.Has(ViewChannel) && s.RulesPending(sub.ID) && need&^p&TimeoutMask != 0 {
		return ErrRulesNotAccepted
	}
	return err
}

// EmptySnapshot knows no channel: everybody but the Owner gets nothing from
// it. Callers use it to fail closed when the real state cannot be loaded.
func EmptySnapshot() *Snapshot { return newSnapshot(time.Time{}) }
