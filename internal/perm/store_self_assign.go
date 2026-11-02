package perm

import (
	"context"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// SelfAssignBlocked are the permissions a self-assignable role cannot hold:
// Administrator, everything that opens the admin area, and the moderation
// powers over other members' messages, Stages and events. A member who may
// choose a role must never be able to choose power over others with it.
const SelfAssignBlocked = Elevated | ManageMessages | ManageStage | ManageEvents

var (
	errSelfAssignElevated = httpx.ErrInvalidInput("a self-assignable role cannot have administrator, admin-area or moderation permissions")
	errSelfAssignPlain    = httpx.ErrInvalidInput("@everyone and managed roles cannot be self-assignable")
	errOverwriteOpenRole  = httpx.ErrInvalidInput("a self-assignable role cannot be allowed administrator, admin-area or moderation permissions in a channel")
	errNotSelfAssignable  = httpx.ErrForbidden("members cannot take or drop this role themselves")
	errOwnerHasNoRoles    = httpx.ErrForbidden("the Owner has no roles to choose")
)

// selfAssignable reports whether members may choose the role for themselves:
// flagged, an ordinary role, and nothing in it is administration or moderation. The last
// condition is checked at every use as well as when the role is saved, so a
// role that somehow gained such a permission stops being open at once.
func (r Role) selfAssignable() bool {
	return r.SelfAssignable && !r.Everyone && !r.Managed && !r.Permissions.HasAny(SelfAssignBlocked)
}

// checkSelfAssignable rejects a role that is flagged but cannot be: the flag
// and the permissions are checked together, so neither can be changed into a
// combination that hands members administration.
func checkSelfAssignable(role Role) error {
	switch {
	case !role.SelfAssignable:
		return nil
	case role.Everyone || role.Managed:
		return errSelfAssignPlain
	case role.Permissions.HasAny(SelfAssignBlocked):
		return errSelfAssignElevated
	}
	return nil
}

// applyRoleSelfAssign applies the description and the self-assign flag of in.
func applyRoleSelfAssign(role Role, in RoleInput) (Role, error) {
	if in.Description != nil {
		d, err := httpx.CleanText("description", *in.Description, maxRoleDescription, false)
		if err != nil {
			return Role{}, err
		}
		role.Description = d
	}
	if in.SelfAssignable != nil {
		role.SelfAssignable = *in.SelfAssignable
	}
	return role, nil
}

type roleExtras struct {
	selfAssignable bool
	description    string
}

// newRoleExtras validates the description and the flag of a role to create.
func newRoleExtras(perms Set, in RoleInput) (roleExtras, error) {
	role, err := applyRoleSelfAssign(Role{Permissions: perms}, in)
	if err != nil {
		return roleExtras{}, err
	}
	if err := checkSelfAssignable(role); err != nil {
		return roleExtras{}, err
	}
	return roleExtras{selfAssignable: role.SelfAssignable, description: role.Description}, nil
}

// SelfAssignableRoles lists the roles members may choose for themselves,
// highest first.
func (s *Service) SelfAssignableRoles(ctx context.Context) ([]Role, error) {
	snap, err := s.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	open := []Role{}
	for _, r := range snap.Roles() {
		if snap.open(r) {
			open = append(open, r)
		}
	}
	return open, nil
}

// SetSelfRole adds the role to the member's own account, or removes it. The
// role must be open to self-assignment; nothing else about the member's roles
// changes. It returns the role, whether the member's roles changed (asking
// for what they already have is not a change and is not audited) and the
// roles the member holds afterwards.
func (s *Service) SetSelfRole(ctx context.Context, userID, roleID uuid.UUID, on bool) (Role, bool, []uuid.UUID, error) {
	var role Role
	var held []uuid.UUID
	changed := false
	err := s.Locked(ctx, Subject{ID: userID}, func(tx pgx.Tx, snap *Snapshot) error {
		var err error
		if role, err = openRole(snap, roleID); err != nil {
			return err
		}
		if err := checkSelfAssignTarget(ctx, tx, userID); err != nil {
			return err
		}
		held = slices.DeleteFunc(slices.Clone(snap.userRoles[userID]), func(id uuid.UUID) bool { return id == roleID && !on })
		if holds(snap, userID, roleID) == on {
			return nil
		}
		changed = true
		if on {
			held = append(held, roleID)
		}
		if on {
			return grantRoles(ctx, tx, userID, []uuid.UUID{roleID})
		}
		return revokeRoles(ctx, tx, userID, []uuid.UUID{roleID})
	})
	if err != nil {
		return Role{}, false, nil, err
	}
	if changed {
		s.Changed([]uuid.UUID{userID})
	}
	return role, changed, held, nil
}

// open reports whether members may choose the role for themselves right now:
// the role itself is open and no channel or category overwrite allows it
// something that is blocked there either (Manage messages in one channel is
// moderation power just the same).
func (s *Snapshot) open(role Role) bool {
	return role.selfAssignable() && !s.roleAllowsInChannels(role.ID, SelfAssignBlocked)
}

// openRole is the role if members may choose it for themselves.
func openRole(snap *Snapshot, id uuid.UUID) (Role, error) {
	role, ok := snap.roles[id]
	switch {
	case !ok:
		return Role{}, errRoleNotFound
	case !snap.open(role):
		return Role{}, errNotSelfAssignable
	}
	return role, nil
}

// checkOverwritesOfOpenRoles refuses overwrites that would allow a
// self-assignable role a blocked permission in a channel.
func checkOverwritesOfOpenRoles(snap *Snapshot, next map[overwriteKey]overwrite) error {
	for k, o := range next {
		if k.typ == "role" && snap.roles[k.id].SelfAssignable && o.allow.HasAny(SelfAssignBlocked) {
			return errOverwriteOpenRole
		}
	}
	return nil
}

// checkSelfAssignTarget refuses accounts that cannot choose roles: bots, the
// deleted and the Owner.
func checkSelfAssignTarget(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	target, err := loadRoleTarget(ctx, tx, userID, false)
	if err != nil {
		return err
	}
	if target.Owner {
		return errOwnerHasNoRoles
	}
	return nil
}

func holds(snap *Snapshot, userID, roleID uuid.UUID) bool {
	return slices.Contains(snap.userRoles[userID], roleID)
}
