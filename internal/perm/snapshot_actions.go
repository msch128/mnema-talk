package perm

import (
	"math"
	"slices"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// TopPosition is the position of sub's highest role (0 without custom roles);
// the Owner is above every position. A role members can assign to themselves
// is cosmetic and never counts: otherwise taking one positioned above the
// moderators would put a member out of their reach.
func (s *Snapshot) TopPosition(sub Subject) int {
	if s.IsOwner(sub) {
		return math.MaxInt32
	}
	top := 0
	for _, id := range s.userRoles[sub.ID] {
		if role := s.roles[id]; !role.SelfAssignable {
			top = max(top, role.Position)
		}
	}
	return top
}

// ErrOwnerProtected is returned for any action aimed at the Owner.
var ErrOwnerProtected = httpx.ErrForbidden("the Owner cannot be targeted")

// CanModerate checks that actor may act on target as a member: never on the
// Owner, and only on members whose highest role is below the actor's.
func (s *Snapshot) CanModerate(actor, target Subject) error {
	if s.IsOwner(target) {
		return ErrOwnerProtected
	}
	if s.IsOwner(actor) || actor.ID == target.ID {
		return nil
	}
	if s.TopPosition(actor) <= s.TopPosition(target) {
		return httpx.ErrForbidden("that member's highest role is not below yours")
	}
	return nil
}

// CanManageRole checks that actor's highest role is above the role.
func (s *Snapshot) CanManageRole(actor Subject, role Role) error {
	if s.IsOwner(actor) {
		return nil
	}
	if s.TopPosition(actor) <= role.Position {
		return httpx.ErrForbidden("that role is not below your highest role")
	}
	return nil
}

// Elevated are the permissions only the Owner may hand to, or manage on, a
// bot: Administrator and everything that opens the admin area.
const Elevated = Administrator | AdminArea

// CanActAs checks that actor may mint a token for, or change the access of,
// the bot: the bot must not hold elevated permissions unless actor is the
// Owner, and everything it may do, server-wide and in every channel it can
// view, must be within what actor may do themselves. Otherwise holding Manage
// bots would be a way to act with rights one does not have.
func (s *Snapshot) CanActAs(actor, bot Subject) error {
	if s.IsOwner(actor) {
		return nil
	}
	g := s.Global(bot)
	if s.ElevatedFor(bot) {
		return httpx.ErrForbidden("only the Owner can manage a bot with administrator permissions")
	}
	if name := s.Global(actor).MissingName(g); name != "" {
		return httpx.ErrForbidden("the bot holds a permission you do not have: " + name)
	}
	for ch, p := range s.ChannelPermissions(bot) {
		if name := s.Channel(actor, ch).MissingName(p); name != "" {
			return httpx.ErrForbidden("the bot holds a permission you do not have in a channel: " + name)
		}
	}
	return nil
}

// RoleHeldByBot reports whether any bot holds the role. The managed Bot role
// counts as held even before the first bot exists: it is every bot's.
func (s *Snapshot) RoleHeldByBot(role uuid.UUID) bool {
	if s.roles[role].Managed {
		return true
	}
	for u, roles := range s.userRoles {
		if s.bots[u] && slices.Contains(roles, role) {
			return true
		}
	}
	return false
}

// ElevatedFor reports whether sub holds an elevated permission, server-wide or
// in any channel (Disconnect members is a channel permission that opens part
// of the admin area).
func (s *Snapshot) ElevatedFor(sub Subject) bool {
	if s.Global(sub).HasAny(Elevated) {
		return true
	}
	for _, p := range s.ChannelPermissions(sub) {
		if p.HasAny(Elevated) {
			return true
		}
	}
	return false
}

// RoleGrantsElevatedInChannels reports whether any category or channel
// overwrite allows the role an elevated permission.
func (s *Snapshot) RoleGrantsElevatedInChannels(role uuid.UUID) bool {
	return s.roleAllowsInChannels(role, Elevated)
}

// roleAllowsInChannels reports whether any category or channel overwrite
// allows the role one of the permissions in mask.
func (s *Snapshot) roleAllowsInChannels(role uuid.UUID, mask Set) bool {
	for _, m := range []map[uuid.UUID]*scope{s.channelOv, s.categoryOv} {
		for _, sc := range m {
			if ow, ok := sc.roles[role]; ok && ow.allow.HasAny(mask) {
				return true
			}
		}
	}
	return false
}
