package chat

import (
	"slices"

	"github.com/google/uuid"
)

// AllowedRoles narrows roles to what a bot or webhook request asked for with
// Discord's allowed_mentions: every role when parse contains "roles", else
// only the roles listed by ID in ids. Without a request no role is mentioned,
// so a role mention is always an explicit choice of the sender; the default
// stays off.
func AllowedRoles(roles []MentionRole, parse, ids []string) []MentionRole {
	if slices.Contains(parse, "roles") {
		return roles
	}
	listed := make(map[uuid.UUID]bool, len(ids))
	for _, raw := range ids {
		if id, err := uuid.Parse(raw); err == nil {
			listed[id] = true
		}
	}
	var out []MentionRole
	for _, r := range roles {
		if listed[r.ID] {
			out = append(out, r)
		}
	}
	return out
}
