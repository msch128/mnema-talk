package perm

import "github.com/google/uuid"

// AsRoleOnly returns the snapshot as seen for a made-up member who holds
// @everyone and the one role, nothing else: no overwrite for them personally,
// no timeout, rules accepted, not the Owner and not a bot. The returned
// Subject answers every question of the snapshot (Channel, CategoryVisible,
// ...) for that member, with the real resolution order. The member exists only
// in the returned copy; the snapshot itself is not changed. It is false when
// the role does not exist.
//
// Passing @everyone's own ID gives a member with no custom role.
func (s *Snapshot) AsRoleOnly(role uuid.UUID) (*Snapshot, Subject, bool) {
	r, ok := s.roles[role]
	if !ok {
		return nil, Subject{}, false
	}
	member := uuid.New()
	view := *s
	view.owners = map[uuid.UUID]bool{}
	view.bots = map[uuid.UUID]bool{}
	view.timeouts = map[uuid.UUID]Timeout{}
	view.userRoles = map[uuid.UUID][]uuid.UUID{}
	if !r.Everyone {
		view.userRoles[member] = []uuid.UUID{role}
	}
	view.rulesAccepted = map[uuid.UUID]int{member: s.rulesVersion}
	return &view, Subject{ID: member}, true
}
