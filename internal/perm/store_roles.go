package perm

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// checkGrant rejects bits the actor does not hold themselves: nobody can
// hand out more than they have.
func checkGrant(snap *Snapshot, actor Subject, changed Set) error {
	if changed.HasAny(OwnerGrantOnly) && !snap.IsOwner(actor) {
		return httpx.ErrForbidden("only the Owner can grant: manage bots")
	}
	if name := snap.Global(actor).MissingName(changed); name != "" {
		return httpx.ErrForbidden("cannot grant a permission you do not have: " + name)
	}
	return nil
}

// checkRoleUpload validates a role's upload override. Only the Owner sets it:
// it is a resource limit, not a permission a role manager may hand out.
// current is kept when the field is absent.
func (s *Service) checkRoleUpload(actor Subject, in OptionalInt, current *int) (*int, error) {
	if !in.Present {
		return current, nil
	}
	if !actor.Owner {
		return nil, httpx.ErrForbidden("only the Owner can set a role's upload limit")
	}
	if in.Value != nil && (*in.Value < 1 || *in.Value > s.UploadCeilingMB()) {
		return nil, httpx.ErrInvalidInput(fmt.Sprintf("max_upload_mb must be between 1 and %d", s.UploadCeilingMB()))
	}
	return in.Value, nil
}

func cleanRoleName(name string) (string, error) {
	name, err := httpx.CleanText("name", name, maxRoleName, true)
	if err != nil {
		return "", err
	}
	if reserved := strings.TrimPrefix(httpx.NameSkeleton(name), "@"); reserved == "everyone" || reserved == "here" {
		return "", httpx.ErrInvalidInput("this role name is reserved")
	}
	return name, nil
}

func checkColor(c int) error {
	if c < 0 || c > 0xFFFFFF {
		return httpx.ErrInvalidInput("color must be between 0 and 16777215")
	}
	return nil
}

func scanRole(row pgx.Row) (Role, error) {
	var r Role
	var perms int64
	err := row.Scan(&r.ID, &r.Name, &r.Color, &r.Position, &perms, &r.Hoist, &r.Mentionable, &r.Everyone, &r.Managed, &r.MaxUploadMB, &r.BotUserID, &r.SelfAssignable, &r.Description)
	r.Permissions = Set(perms)
	return r, err
}

const roleColumns = `id, name, color, position, permissions, hoist, mentionable, is_everyone, managed, max_upload_mb, bot_user_id, self_assignable, description`

// CreateRole adds a role at the bottom of the hierarchy (just above
// @everyone), so it is always below the actor's own highest role.
func (s *Service) CreateRole(ctx context.Context, actor Subject, in RoleInput) (Role, error) {
	var created Role
	err := s.mutate(ctx, actor, ManageRoles, func(tx pgx.Tx, snap *Snapshot) ([]uuid.UUID, error) {
		if len(snap.roles) >= maxRoles {
			return nil, httpx.ErrConflict("too many roles")
		}
		name, color, perms, err := checkNewRole(snap, actor, in)
		if err != nil {
			return nil, err
		}
		extra, err := newRoleExtras(perms, in)
		if err != nil {
			return nil, err
		}
		maxUpload, err := s.checkRoleUpload(actor, in.MaxUploadMB, nil)
		if err != nil {
			return nil, err
		}
		at := newRolePosition(snap, actor)
		if _, err := tx.Exec(ctx, `UPDATE roles SET position = position + 1 WHERE NOT is_everyone AND position >= $1`, at); err != nil {
			return nil, fmt.Errorf("shift roles: %w", err)
		}
		created, err = scanRole(tx.QueryRow(ctx, `
			INSERT INTO roles (name, color, position, permissions, hoist, mentionable, max_upload_mb, self_assignable, description)
			VALUES ($1, $2, $7, $3, $4, $5, $6, $8, $9) RETURNING `+roleColumns,
			name, color, int64(perms), in.Hoist != nil && *in.Hoist, in.Mentionable != nil && *in.Mentionable, maxUpload, at, extra.selfAssignable, extra.description))
		if err != nil {
			return nil, fmt.Errorf("insert role: %w", err)
		}
		return []uuid.UUID{}, nil // nobody holds it yet
	})
	return created, err
}

// checkNewRole validates the name, colour and permissions of a role to create
// (absent fields default to empty) and that the actor may hand those out.
func checkNewRole(snap *Snapshot, actor Subject, in RoleInput) (name string, color int, perms Set, err error) {
	if in.Name != nil {
		name = *in.Name
	}
	if name, err = cleanRoleName(name); err != nil {
		return "", 0, 0, err
	}
	if in.Color != nil {
		color = *in.Color
	}
	if err = checkColor(color); err != nil {
		return "", 0, 0, err
	}
	if in.Permissions != nil {
		perms = *in.Permissions
	}
	if !perms.Valid() {
		return "", 0, 0, httpx.ErrInvalidInput("unknown permission bits")
	}
	if err = checkGrant(snap, actor, perms); err != nil {
		return "", 0, 0, err
	}
	return name, color, perms, nil
}

// newRolePosition is where a new role goes: to the bottom, except that it
// stays above the managed block (the shared Bot role and the bots' own roles)
// while that block sits at the bottom: members who manage bots must outrank
// the roles bots hold. Only the unbroken run of managed roles from position 1
// counts; once a custom role lies below a managed one, nothing is skipped.
// Whatever the block, the role stays below the actor's highest role.
func newRolePosition(snap *Snapshot, actor Subject) int {
	managedAt := map[int]bool{}
	for _, r := range snap.roles {
		if !r.Everyone && r.Managed {
			managedAt[r.Position] = true
		}
	}
	at := 1
	for managedAt[at] {
		at++
	}
	if top := snap.TopPosition(actor); !snap.IsOwner(actor) && top >= 1 && at > top {
		at = top
	}
	return at
}

var (
	errRoleNotFound = httpx.ErrNotFound("role not found")
	errManagedName  = httpx.ErrInvalidInput("this role is managed: it cannot be renamed")
	errManagedHeld  = httpx.ErrInvalidInput("this role is managed: only bots hold it")
	errOwnRole      = httpx.ErrInvalidInput("a bot's own role belongs to that bot only")
)

// UpdateRole changes a role. @everyone only has its permissions editable.
func (s *Service) UpdateRole(ctx context.Context, actor Subject, id uuid.UUID, in RoleInput) (Role, error) {
	var updated Role
	err := s.mutate(ctx, actor, ManageRoles, func(tx pgx.Tx, snap *Snapshot) ([]uuid.UUID, error) {
		var holders []uuid.UUID
		var err error
		updated, holders, err = s.updateRole(ctx, tx, snap, actor, id, in)
		return holders, err
	})
	return updated, err
}

func checkBotRoleElevation(snap *Snapshot, actor Subject, id uuid.UUID, oldPerms, newPerms Set) error {
	added := newPerms &^ oldPerms
	if added.HasAny(Elevated) && !snap.IsOwner(actor) && snap.RoleHeldByBot(id) {
		return httpx.ErrForbidden("a bot holds this role: only the Owner can add administrator or admin-area permissions to it")
	}
	return nil
}

func (s *Service) validateRoleUpdate(snap *Snapshot, actor Subject, id uuid.UUID, in RoleInput) (Role, Role, error) {
	role, ok := snap.roles[id]
	if !ok {
		return Role{}, Role{}, errRoleNotFound
	}
	if err := checkRoleEditable(snap, actor, role, in); err != nil {
		return Role{}, Role{}, err
	}
	next, err := applyRoleInput(role, in)
	if err != nil {
		return Role{}, Role{}, err
	}
	if err := checkSelfAssignable(next); err != nil {
		return Role{}, Role{}, err
	}
	if next.SelfAssignable && snap.roleAllowsInChannels(id, SelfAssignBlocked) {
		return Role{}, Role{}, errOverwriteOpenRole
	}
	if err := checkGrant(snap, actor, role.Permissions^next.Permissions); err != nil {
		return Role{}, Role{}, err
	}
	if err := checkBotRoleElevation(snap, actor, id, role.Permissions, next.Permissions); err != nil {
		return Role{}, Role{}, err
	}
	next.MaxUploadMB, err = s.checkRoleUpload(actor, in.MaxUploadMB, role.MaxUploadMB)
	if err != nil {
		return Role{}, Role{}, err
	}
	return role, next, nil
}

// updateRole validates and stores the change; the second result lists whose
// permissions change with it (nil: everybody).
func (s *Service) updateRole(ctx context.Context, tx pgx.Tx, snap *Snapshot, actor Subject, id uuid.UUID, in RoleInput) (Role, []uuid.UUID, error) {
	role, next, err := s.validateRoleUpdate(snap, actor, id, in)
	if err != nil {
		return Role{}, nil, err
	}
	updated, err := scanRole(tx.QueryRow(ctx, `
		UPDATE roles SET name = $2, color = $3, permissions = $4, hoist = $5, mentionable = $6, max_upload_mb = $7, self_assignable = $8, description = $9, updated_at = NOW()
		WHERE id = $1 RETURNING `+roleColumns,
		id, next.Name, next.Color, int64(next.Permissions), next.Hoist, next.Mentionable, next.MaxUploadMB, next.SelfAssignable, next.Description))
	if err != nil {
		return Role{}, nil, fmt.Errorf("update role: %w", err)
	}
	if role.Everyone {
		return updated, nil, nil
	}
	holders, err := roleHolders(ctx, tx, id)
	return updated, holders, err
}

func checkEveryoneChanges(in RoleInput) error {
	if in.Name != nil || in.Color != nil {
		return httpx.ErrInvalidInput("only the permissions of @everyone can be changed")
	}
	if in.Hoist != nil || in.Mentionable != nil || in.SelfAssignable != nil || in.Description != nil {
		return httpx.ErrInvalidInput("only the permissions of @everyone can be changed")
	}
	return nil
}

// checkRoleEditable lets @everyone change its permissions only and any other
// role only for actors above it.
func checkRoleEditable(snap *Snapshot, actor Subject, role Role, in RoleInput) error {
	if !role.Everyone {
		return snap.CanManageRole(actor, role)
	}
	return checkEveryoneChanges(in)
}

// applyRoleInput returns the role with the fields of in applied, after
// validating them. The upload limit is left to checkRoleUpload.
func applyRoleInput(role Role, in RoleInput) (Role, error) {
	next, err := applyRoleAppearance(role, in)
	if err != nil {
		return Role{}, err
	}
	if in.Permissions != nil {
		if !in.Permissions.Valid() {
			return Role{}, httpx.ErrInvalidInput("unknown permission bits")
		}
		next.Permissions = *in.Permissions
	}
	if in.Hoist != nil {
		next.Hoist = *in.Hoist
	}
	if in.Mentionable != nil {
		next.Mentionable = *in.Mentionable
	}
	return applyRoleSelfAssign(next, in)
}

func resolveRoleName(role Role, inName *string) (string, error) {
	if inName == nil {
		return role.Name, nil
	}
	name, err := cleanRoleName(*inName)
	if err != nil {
		return "", err
	}
	if role.Managed && name != role.Name {
		return "", errManagedName
	}
	return name, nil
}

func resolveRoleColor(role Role, inColor *int) (int, error) {
	if inColor == nil {
		return role.Color, nil
	}
	if err := checkColor(*inColor); err != nil {
		return 0, err
	}
	return *inColor, nil
}

// applyRoleAppearance applies name and colour; a managed role keeps its name
// and has no upload limit of its own.
func applyRoleAppearance(role Role, in RoleInput) (Role, error) {
	if role.Managed && in.MaxUploadMB.Present {
		return Role{}, httpx.ErrInvalidInput("managed bot roles have no upload limit: bots upload under their own rules")
	}
	name, err := resolveRoleName(role, in.Name)
	if err != nil {
		return Role{}, err
	}
	color, err := resolveRoleColor(role, in.Color)
	if err != nil {
		return Role{}, err
	}
	next := role
	next.Name = name
	next.Color = color
	return next, nil
}

func roleHolders(ctx context.Context, tx pgx.Tx, id uuid.UUID) ([]uuid.UUID, error) {
	rows, err := tx.Query(ctx, `SELECT user_id FROM user_roles WHERE role_id = $1`, id)
	if err != nil {
		return nil, fmt.Errorf("role holders: %w", err)
	}
	// Never nil: nil would mean "everybody" to the notifier.
	holders, err := pgx.AppendRows([]uuid.UUID{}, rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, fmt.Errorf("role holders: %w", err)
	}
	return holders, nil
}

// DeleteRole removes a role from everyone who has it and closes the gap in
// the hierarchy.
func (s *Service) DeleteRole(ctx context.Context, actor Subject, id uuid.UUID) error {
	return s.mutate(ctx, actor, ManageRoles, func(tx pgx.Tx, snap *Snapshot) ([]uuid.UUID, error) {
		role, err := deletableRole(snap, actor, id)
		if err != nil {
			return nil, err
		}
		holders, err := roleHolders(ctx, tx, id)
		if err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM roles WHERE id = $1`, id); err != nil {
			return nil, fmt.Errorf("delete role: %w", err)
		}
		if _, err := tx.Exec(ctx, `UPDATE roles SET position = position - 1 WHERE NOT is_everyone AND position > $1`, role.Position); err != nil {
			return nil, fmt.Errorf("close role gap: %w", err)
		}
		return holders, nil
	})
}

// deletableRole returns the role if the actor may delete it: neither
// @everyone nor a managed role, and below the actor.
func deletableRole(snap *Snapshot, actor Subject, id uuid.UUID) (Role, error) {
	role, ok := snap.roles[id]
	switch {
	case !ok:
		return Role{}, errRoleNotFound
	case role.Everyone:
		return Role{}, httpx.ErrInvalidInput("@everyone cannot be deleted")
	case role.Managed:
		return Role{}, httpx.ErrInvalidInput("this role is managed: it cannot be deleted")
	}
	if err := snap.CanManageRole(actor, role); err != nil {
		return Role{}, err
	}
	return role, nil
}
