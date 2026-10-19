package perm

import (
	"context"
	"fmt"
	"slices"
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
	if strings.EqualFold(strings.TrimPrefix(name, "@"), "everyone") || strings.EqualFold(strings.TrimPrefix(name, "@"), "here") {
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
	err := row.Scan(&r.ID, &r.Name, &r.Color, &r.Position, &perms, &r.Hoist, &r.Mentionable, &r.Everyone, &r.Managed, &r.MaxUploadMB, &r.BotUserID)
	r.Permissions = Set(perms)
	return r, err
}

const roleColumns = `id, name, color, position, permissions, hoist, mentionable, is_everyone, managed, max_upload_mb, bot_user_id`

// CreateRole adds a role at the bottom of the hierarchy (just above
// @everyone), so it is always below the actor's own highest role.
func (s *Service) CreateRole(ctx context.Context, actor Subject, in RoleInput) (Role, error) {
	var created Role
	err := s.mutate(ctx, actor, ManageRoles, func(tx pgx.Tx, snap *Snapshot) ([]uuid.UUID, error) {
		if len(snap.roles) > maxRoles {
			return nil, httpx.ErrConflict("too many roles")
		}
		name := ""
		if in.Name != nil {
			name = *in.Name
		}
		name, err := cleanRoleName(name)
		if err != nil {
			return nil, err
		}
		color := 0
		if in.Color != nil {
			color = *in.Color
		}
		if err := checkColor(color); err != nil {
			return nil, err
		}
		var perms Set
		if in.Permissions != nil {
			perms = *in.Permissions
		}
		if !perms.Valid() {
			return nil, httpx.ErrInvalidInput("unknown permission bits")
		}
		if err := checkGrant(snap, actor, perms); err != nil {
			return nil, err
		}
		maxUpload, err := s.checkRoleUpload(actor, in.MaxUploadMB, nil)
		if err != nil {
			return nil, err
		}
		// A new role goes to the bottom, except that it stays above the managed
		// block (the shared Bot role and the bots' own roles) while that block
		// sits at the bottom: members who manage bots must outrank the roles
		// bots hold. Only the unbroken run of managed roles from position 1
		// counts; once a custom role lies below a managed one, nothing is
		// skipped. Whatever the block, the role stays below the actor's
		// highest role.
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
		if _, err := tx.Exec(ctx, `UPDATE roles SET position = position + 1 WHERE NOT is_everyone AND position >= $1`, at); err != nil {
			return nil, fmt.Errorf("shift roles: %w", err)
		}
		created, err = scanRole(tx.QueryRow(ctx, `
			INSERT INTO roles (name, color, position, permissions, hoist, mentionable, max_upload_mb)
			VALUES ($1, $2, $7, $3, $4, $5, $6) RETURNING `+roleColumns,
			name, color, int64(perms), in.Hoist != nil && *in.Hoist, in.Mentionable != nil && *in.Mentionable, maxUpload, at))
		if err != nil {
			return nil, fmt.Errorf("insert role: %w", err)
		}
		return []uuid.UUID{}, nil // nobody holds it yet
	})
	return created, err
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
		role, ok := snap.roles[id]
		if !ok {
			return nil, errRoleNotFound
		}
		if role.Everyone {
			if in.Name != nil || in.Color != nil || in.Hoist != nil || in.Mentionable != nil {
				return nil, httpx.ErrInvalidInput("only the permissions of @everyone can be changed")
			}
		} else if err := snap.CanManageRole(actor, role); err != nil {
			return nil, err
		}
		next := role
		if in.Name != nil {
			name, err := cleanRoleName(*in.Name)
			if err != nil {
				return nil, err
			}
			if role.Managed && name != role.Name {
				return nil, errManagedName
			}
			next.Name = name
		}
		if role.Managed && in.MaxUploadMB.Present {
			return nil, httpx.ErrInvalidInput("managed bot roles have no upload limit: bots upload under their own rules")
		}
		if in.Color != nil {
			if err := checkColor(*in.Color); err != nil {
				return nil, err
			}
			next.Color = *in.Color
		}
		if in.Permissions != nil {
			if !in.Permissions.Valid() {
				return nil, httpx.ErrInvalidInput("unknown permission bits")
			}
			next.Permissions = *in.Permissions
		}
		if in.Hoist != nil {
			next.Hoist = *in.Hoist
		}
		if in.Mentionable != nil {
			next.Mentionable = *in.Mentionable
		}
		// Only the bits that change need the actor's own authority.
		if err := checkGrant(snap, actor, role.Permissions^next.Permissions); err != nil {
			return nil, err
		}
		// A role a bot already holds must not become a way to give it
		// administrator power: that stays with the Owner.
		if added := next.Permissions &^ role.Permissions; added.HasAny(Elevated) && !snap.IsOwner(actor) && snap.RoleHeldByBot(id) {
			return nil, httpx.ErrForbidden("a bot holds this role: only the Owner can add administrator or admin-area permissions to it")
		}
		var err error
		next.MaxUploadMB, err = s.checkRoleUpload(actor, in.MaxUploadMB, role.MaxUploadMB)
		if err != nil {
			return nil, err
		}
		updated, err = scanRole(tx.QueryRow(ctx, `
			UPDATE roles SET name = $2, color = $3, permissions = $4, hoist = $5, mentionable = $6, max_upload_mb = $7, updated_at = NOW()
			WHERE id = $1 RETURNING `+roleColumns,
			id, next.Name, next.Color, int64(next.Permissions), next.Hoist, next.Mentionable, next.MaxUploadMB))
		if err != nil {
			return nil, fmt.Errorf("update role: %w", err)
		}
		if role.Everyone {
			return nil, nil
		}
		return roleHolders(ctx, tx, id)
	})
	return updated, err
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
		role, ok := snap.roles[id]
		if !ok || role.Everyone {
			if ok {
				return nil, httpx.ErrInvalidInput("@everyone cannot be deleted")
			}
			return nil, errRoleNotFound
		}
		if role.Managed {
			return nil, httpx.ErrInvalidInput("this role is managed: it cannot be deleted")
		}
		if err := snap.CanManageRole(actor, role); err != nil {
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

// ReorderRoles sets the hierarchy: ids lists every custom role, highest
// first. A member can only move roles that are below their own highest role
// and only to places below it; roles at or above it keep their position.
func (s *Service) ReorderRoles(ctx context.Context, actor Subject, ids []uuid.UUID) error {
	return s.mutate(ctx, actor, ManageRoles, func(tx pgx.Tx, snap *Snapshot) ([]uuid.UUID, error) {
		// A bot's own role is not part of the hierarchy people arrange: it
		// stays in the managed block at the bottom whatever the list says.
		custom := 0
		for _, r := range snap.roles {
			if !r.Everyone && r.BotUserID == nil {
				custom++
			}
		}
		// A list written before the Bot role existed leaves it out: it goes to
		// the bottom, where it started. Own roles of bots are ignored.
		ids = slices.DeleteFunc(slices.Clone(ids), func(id uuid.UUID) bool { return snap.roles[id].BotUserID != nil })
		for id, r := range snap.roles {
			if r.Managed && r.BotUserID == nil && !slices.Contains(ids, id) {
				ids = append(ids, id)
			}
		}
		if len(ids) != custom {
			return nil, httpx.ErrInvalidInput("list every role exactly once")
		}
		seen := map[uuid.UUID]bool{}
		top := snap.TopPosition(actor)
		for i, id := range ids {
			role, ok := snap.roles[id]
			if !ok || role.Everyone || seen[id] {
				return nil, httpx.ErrInvalidInput("list every role exactly once")
			}
			seen[id] = true
			pos := custom - i
			if pos == role.Position {
				continue
			}
			if !snap.IsOwner(actor) && (role.Position >= top || pos >= top) {
				return nil, httpx.ErrForbidden("roles at or above your highest role cannot be moved")
			}
			if _, err := tx.Exec(ctx, `UPDATE roles SET position = $2, updated_at = NOW() WHERE id = $1`, id, pos); err != nil {
				return nil, fmt.Errorf("move role: %w", err)
			}
		}
		return nil, nil
	})
}

// Roles lists every role, highest first.
func (s *Service) Roles(ctx context.Context) ([]Role, error) {
	snap, err := s.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	return snap.Roles(), nil
}
