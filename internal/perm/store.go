package perm

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/httpx"
)

const (
	// lockKey serializes every permission change (pg_advisory_xact_lock), so
	// the hierarchy checks below see the state they write into.
	lockKey = 0x6d6e7065726d // "mnperm"

	maxRoles      = 250
	maxOverwrites = 200
	maxRoleName   = 32
)

// RoleInput is a role to create or the fields of one to change; omitted
// fields are kept (or defaulted on create).
type RoleInput struct {
	Name        *string `json:"name" minLength:"1" maxLength:"32" binding:"optional"`
	Color       *int    `json:"color" minimum:"0" maximum:"16777215" binding:"optional"`
	Permissions *Set    `json:"permissions" swaggertype:"integer" minimum:"0" binding:"optional"`
	Hoist       *bool   `json:"hoist" binding:"optional"`
	Mentionable *bool   `json:"mentionable" binding:"optional"`
}

// mutate runs fn under the permission lock with the state as it is inside the
// transaction, then publishes the change for the users fn reports (nil =
// everybody).
func (s *Service) mutate(ctx context.Context, actor Subject, need Set, fn func(tx pgx.Tx, snap *Snapshot) ([]uuid.UUID, error)) error {
	var affected []uuid.UUID
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, int64(lockKey)); err != nil {
			return fmt.Errorf("lock permissions: %w", err)
		}
		snap := newSnapshot(s.now())
		if err := snap.read(ctx, tx); err != nil {
			return fmt.Errorf("load permissions: %w", err)
		}
		if name := snap.Global(actor).MissingName(need); name != "" {
			return httpx.ErrForbidden("missing permission: " + name)
		}
		var err error
		affected, err = fn(tx, snap)
		return err
	})
	if err != nil {
		return err
	}
	s.Changed(affected)
	return nil
}

// checkGrant rejects bits the actor does not hold themselves: nobody can
// hand out more than they have.
func checkGrant(snap *Snapshot, actor Subject, changed Set) error {
	if name := snap.Global(actor).MissingName(changed); name != "" {
		return httpx.ErrForbidden("cannot grant a permission you do not have: " + name)
	}
	return nil
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
	err := row.Scan(&r.ID, &r.Name, &r.Color, &r.Position, &perms, &r.Hoist, &r.Mentionable, &r.Everyone)
	r.Permissions = Set(perms)
	return r, err
}

const roleColumns = `id, name, color, position, permissions, hoist, mentionable, is_everyone`

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
		if _, err := tx.Exec(ctx, `UPDATE roles SET position = position + 1 WHERE NOT is_everyone`); err != nil {
			return nil, fmt.Errorf("shift roles: %w", err)
		}
		created, err = scanRole(tx.QueryRow(ctx, `
			INSERT INTO roles (name, color, position, permissions, hoist, mentionable)
			VALUES ($1, $2, 1, $3, $4, $5) RETURNING `+roleColumns,
			name, color, int64(perms), in.Hoist != nil && *in.Hoist, in.Mentionable != nil && *in.Mentionable))
		if err != nil {
			return nil, fmt.Errorf("insert role: %w", err)
		}
		return []uuid.UUID{}, nil // nobody holds it yet
	})
	return created, err
}

var errRoleNotFound = httpx.ErrNotFound("role not found")

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
			next.Name = name
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
		var err error
		updated, err = scanRole(tx.QueryRow(ctx, `
			UPDATE roles SET name = $2, color = $3, permissions = $4, hoist = $5, mentionable = $6, updated_at = NOW()
			WHERE id = $1 RETURNING `+roleColumns,
			id, next.Name, next.Color, int64(next.Permissions), next.Hoist, next.Mentionable))
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
		custom := 0
		for _, r := range snap.roles {
			if !r.Everyone {
				custom++
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

// SetMemberRoles replaces the custom roles of a member. Only roles below the
// actor's highest role can be added or removed, only on members whose highest
// role is below it, and never on the Owner.
func (s *Service) SetMemberRoles(ctx context.Context, actor Subject, userID uuid.UUID, roleIDs []uuid.UUID) error {
	return s.mutate(ctx, actor, ManageRoles, func(tx pgx.Tx, snap *Snapshot) ([]uuid.UUID, error) {
		var role string
		err := tx.QueryRow(ctx, `SELECT role FROM users WHERE id = $1`, userID).Scan(&role)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, httpx.ErrNotFound("user not found")
		}
		if err != nil {
			return nil, fmt.Errorf("load user: %w", err)
		}
		target := Subject{ID: userID, Owner: role == "admin"}
		if err := snap.CanModerate(actor, target); err != nil {
			return nil, err
		}
		want := map[uuid.UUID]bool{}
		for _, id := range roleIDs {
			r, ok := snap.roles[id]
			if !ok || r.Everyone {
				return nil, httpx.ErrInvalidInput("unknown role")
			}
			want[id] = true
		}
		have := map[uuid.UUID]bool{}
		for _, id := range snap.userRoles[userID] {
			have[id] = true
		}
		for id := range want {
			if !have[id] {
				if err := snap.CanManageRole(actor, snap.roles[id]); err != nil {
					return nil, err
				}
				if _, err := tx.Exec(ctx, `INSERT INTO user_roles (user_id, role_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, userID, id); err != nil {
					return nil, fmt.Errorf("assign role: %w", err)
				}
			}
		}
		for id := range have {
			if !want[id] {
				// CanModerate above guarantees every role the target holds is
				// below the actor's highest role, so removing it is allowed.
				if _, err := tx.Exec(ctx, `DELETE FROM user_roles WHERE user_id = $1 AND role_id = $2`, userID, id); err != nil {
					return nil, fmt.Errorf("remove role: %w", err)
				}
			}
		}
		return []uuid.UUID{userID}, nil
	})
}

// Overwrite is the allow/deny of one role or member on a category or channel.
type Overwrite struct {
	// Type is what ID refers to.
	Type string `json:"type" enums:"role,member"`
	// ID is the role's or the member's ID; the @everyone role is a role.
	ID    uuid.UUID `json:"id" format:"uuid"`
	Allow Set       `json:"allow" swaggertype:"integer" minimum:"0"`
	Deny  Set       `json:"deny" swaggertype:"integer" minimum:"0"`
}

// Overwrite targets.
const (
	TargetChannel  = "channel"
	TargetCategory = "category"
)

var targetColumns = map[string]string{TargetChannel: "channel_id", TargetCategory: "category_id"}

// Overwrites lists the overwrites set directly on a category or channel.
func (s *Service) Overwrites(ctx context.Context, kind string, id uuid.UUID) ([]Overwrite, error) {
	col, ok := targetColumns[kind]
	if !ok {
		return nil, httpx.ErrInvalidInput("unknown target")
	}
	snap, err := s.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	if kind == TargetChannel && !snap.KnowsChannel(id) || kind == TargetCategory && !snap.KnowsCategory(id) {
		snap, err = s.reload(ctx, true)
		if err != nil {
			return nil, err
		}
		if kind == TargetChannel && !snap.KnowsChannel(id) || kind == TargetCategory && !snap.KnowsCategory(id) {
			return nil, httpx.ErrNotFound(kind + " not found")
		}
	}
	rows, err := s.db.Query(ctx, `SELECT role_id, user_id, allow, deny FROM permission_overwrites WHERE `+col+` = $1 ORDER BY id`, id)
	if err != nil {
		return nil, fmt.Errorf("list overwrites: %w", err)
	}
	type row struct {
		Role, User  *uuid.UUID
		Allow, Deny int64
	}
	found, err := pgx.AppendRows([]row{}, rows, pgx.RowToStructByPos[row])
	if err != nil {
		return nil, fmt.Errorf("list overwrites: %w", err)
	}
	out := make([]Overwrite, 0, len(found))
	for _, r := range found {
		o := Overwrite{Type: "member", Allow: Set(r.Allow), Deny: Set(r.Deny)}
		if r.Role != nil {
			o.Type, o.ID = "role", *r.Role
		} else {
			o.ID = *r.User
		}
		out = append(out, o)
	}
	return out, nil
}

// SetOverwrites replaces the overwrites of a category or channel. The actor
// needs ManageChannels and must see the target; they can only allow what
// they hold there themselves, and the Owner cannot be the subject of one.
func (s *Service) SetOverwrites(ctx context.Context, actor Subject, kind string, id uuid.UUID, list []Overwrite) error {
	col, ok := targetColumns[kind]
	if !ok {
		return httpx.ErrInvalidInput("unknown target")
	}
	if len(list) > maxOverwrites {
		return httpx.ErrInvalidInput("too many overwrites")
	}
	return s.mutate(ctx, actor, ManageChannels, func(tx pgx.Tx, snap *Snapshot) ([]uuid.UUID, error) {
		var existing *scope
		var held Set
		if kind == TargetChannel {
			if !snap.KnowsChannel(id) {
				return nil, httpx.ErrNotFound("channel not found")
			}
			held = snap.Channel(actor, id)
			if !held.Has(ViewChannel) {
				return nil, ErrChannelHidden
			}
			existing = snap.channelOv[id]
		} else {
			if !snap.KnowsCategory(id) {
				return nil, httpx.ErrNotFound("category not found")
			}
			if !snap.CategoryVisible(actor, id) {
				return nil, httpx.ErrNotFound("category not found")
			}
			held = snap.Global(actor)
			existing = snap.categoryOv[id]
		}
		type key struct {
			typ string
			id  uuid.UUID
		}
		dup := map[key]bool{}
		for _, o := range list {
			k := key{o.Type, o.ID}
			if dup[k] {
				return nil, httpx.ErrInvalidInput("duplicate overwrite")
			}
			dup[k] = true
			if !o.Allow.Valid() || !o.Deny.Valid() || (o.Allow|o.Deny)&^ChannelScoped != 0 {
				return nil, httpx.ErrInvalidInput("these permissions cannot be overwritten per channel")
			}
			if o.Allow&o.Deny != 0 {
				return nil, httpx.ErrInvalidInput("a permission cannot be both allowed and denied")
			}
			var before overwrite
			switch o.Type {
			case "role":
				if _, ok := snap.roles[o.ID]; !ok {
					return nil, httpx.ErrInvalidInput("unknown role")
				}
				if existing != nil {
					before = existing.roles[o.ID]
				}
			case "member":
				var role string
				err := tx.QueryRow(ctx, `SELECT role FROM users WHERE id = $1`, o.ID).Scan(&role)
				if errors.Is(err, pgx.ErrNoRows) {
					return nil, httpx.ErrInvalidInput("unknown member")
				}
				if err != nil {
					return nil, fmt.Errorf("load member: %w", err)
				}
				if role == "admin" || snap.owners[o.ID] {
					return nil, ErrOwnerProtected
				}
				if existing != nil {
					before = existing.members[o.ID]
				}
			default:
				return nil, httpx.ErrInvalidInput("type must be role or member")
			}
			if name := held.MissingName(o.Allow &^ before.allow); name != "" {
				return nil, httpx.ErrForbidden("cannot allow a permission you do not have: " + name)
			}
		}
		if _, err := tx.Exec(ctx, `DELETE FROM permission_overwrites WHERE `+col+` = $1`, id); err != nil {
			return nil, fmt.Errorf("clear overwrites: %w", err)
		}
		for _, o := range list {
			if o.Allow == 0 && o.Deny == 0 {
				continue
			}
			roleCol := "role_id"
			if o.Type == "member" {
				roleCol = "user_id"
			}
			if _, err := tx.Exec(ctx, `INSERT INTO permission_overwrites (`+col+`, `+roleCol+`, allow, deny) VALUES ($1, $2, $3, $4)`,
				id, o.ID, int64(o.Allow), int64(o.Deny)); err != nil {
				return nil, fmt.Errorf("insert overwrite: %w", err)
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

// MemberRoles returns the custom role IDs of every member that has any.
func (s *Service) MemberRoles(ctx context.Context) (map[uuid.UUID][]uuid.UUID, error) {
	snap, err := s.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID][]uuid.UUID, len(snap.userRoles))
	for id, list := range snap.userRoles {
		out[id] = slices.Clone(list)
	}
	return out, nil
}
