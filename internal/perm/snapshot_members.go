package perm

import (
	"slices"

	"github.com/google/uuid"
)

// RoleMembers lists the accounts holding the custom role. @everyone has no
// explicit members; it returns nothing for it.
func (s *Snapshot) RoleMembers(role uuid.UUID) []uuid.UUID {
	var out []uuid.UUID
	for user, roles := range s.userRoles {
		if slices.Contains(roles, role) {
			out = append(out, user)
		}
	}
	return out
}
