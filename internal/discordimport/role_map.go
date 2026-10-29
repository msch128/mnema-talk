package discordimport

import (
	"strings"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// RoleChoice is an existing Mnema role a Discord role can be mapped onto.
type RoleChoice struct {
	ID   uuid.UUID `json:"id" format:"uuid"`
	Name string    `json:"name"`
}

// mapError says why the importing member may not map a Discord role onto
// role, or nil when they may. The rules are those of the role editor: the
// role must be below the member's highest role, @everyone and the managed
// Bot roles are never a target, and a role a bot holds is the Owner's (the
// overwrites an import writes could otherwise lift a bot's rights).
func mapError(snap *perm.Snapshot, sub perm.Subject, role perm.Role) error {
	switch {
	case role.Everyone:
		return httpx.ErrInvalidInput("@everyone cannot be a mapping target; it is never changed")
	case role.Managed:
		return httpx.ErrForbidden("the role " + role.Name + " is managed by the server and cannot be a mapping target")
	case snap.RoleHeldByBot(role.ID) && !snap.IsOwner(sub):
		return httpx.ErrForbidden("only the Owner can map onto a role a bot holds: " + role.Name)
	}
	return snap.CanManageRole(sub, role)
}

// mappableRoles lists the roles the member may map onto, highest first.
func mappableRoles(snap *perm.Snapshot, sub perm.Subject) []RoleChoice {
	var out []RoleChoice
	for _, r := range snap.Roles() {
		if mapError(snap, sub, r) == nil {
			out = append(out, RoleChoice{ID: r.ID, Name: r.Name})
		}
	}
	return out
}

// resolveRoleMap validates the requested mapping (Discord role id to Mnema
// role id) against the template and the current roles.
func resolveRoleMap(tpl *Template, snap *perm.Snapshot, sub perm.Subject, raw map[string]string) (map[string]RoleChoice, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	source := map[string]bool{}
	for _, r := range tpl.Guild.Roles {
		source[string(r.ID)] = r.Name != "@everyone"
	}
	out := make(map[string]RoleChoice, len(raw))
	used := map[uuid.UUID]bool{}
	for src, target := range raw {
		if !source[src] {
			return nil, httpx.ErrInvalidInput("role_map: the template has no mappable role " + truncate(src, 40))
		}
		id, err := uuid.Parse(target)
		if err != nil {
			return nil, httpx.ErrInvalidInput("role_map: not a role id")
		}
		role, ok := snap.Role(id)
		if !ok {
			return nil, httpx.ErrInvalidInput("role_map: the role does not exist in this community")
		}
		if err := mapError(snap, sub, role); err != nil {
			return nil, err
		}
		if used[id] {
			return nil, httpx.ErrInvalidInput("role_map: two Discord roles cannot map onto " + role.Name)
		}
		used[id] = true
		out[src] = RoleChoice{ID: id, Name: role.Name}
	}
	return out, nil
}

// suggestions finds, for each name, the existing role with the same name
// (case-insensitive); the first, highest one wins.
func suggestions(choices []RoleChoice) map[string]RoleChoice {
	out := map[string]RoleChoice{}
	for _, c := range choices {
		key := strings.ToLower(strings.TrimSpace(c.Name))
		if _, dup := out[key]; !dup {
			out[key] = c
		}
	}
	return out
}
