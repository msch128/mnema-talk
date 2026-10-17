package perm

import (
	"context"
	"encoding/json"
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
	// MaxUploadMB: absent leaves it alone, null inherits the global limit, a
	// number sets the role's limit. Only the Owner may send it.
	MaxUploadMB OptionalInt `json:"max_upload_mb" swaggertype:"integer" minimum:"1" binding:"optional" extensions:"x-nullable"`
}

// OptionalInt distinguishes an absent JSON field from an explicit null.
type OptionalInt struct {
	Present bool
	Value   *int
}

// UnmarshalJSON records that the field was sent, and its value (nil for null).
func (o *OptionalInt) UnmarshalJSON(b []byte) error {
	o.Present = true
	if string(b) == "null" {
		o.Value = nil
		return nil
	}
	var v int
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	o.Value = &v
	return nil
}

// Locked runs fn in a transaction that holds the permission lock, with the
// permission state as it is inside that transaction. Anything whose decision
// depends on roles or overwrites (moderating a member, moving a channel,
// deleting a category) decides and writes here, so a concurrent permission
// change cannot slip in between the check and the write.
//
// Every permission change runs through here, which makes it the one place for
// the bot invariant (checkBotGrowth): nobody but the Owner can cause a bot to
// gain an elevated permission anywhere, or any permission, server-wide or in
// a channel, that they lack themselves. The outcome is compared, not the
// individual edit, so any path (role bits, positions, memberships,
// overwrites, removing a deny, a channel moving between categories) is
// covered, now and later.
func (s *Service) Locked(ctx context.Context, actor Subject, fn func(tx pgx.Tx, snap *Snapshot) error) error {
	if s.db == nil {
		return errNoDatabase
	}
	return pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, int64(lockKey)); err != nil {
			return fmt.Errorf("lock permissions: %w", err)
		}
		snap := newSnapshot(s.now())
		if err := snap.read(ctx, tx); err != nil {
			return fmt.Errorf("load permissions: %w", err)
		}
		if err := fn(tx, snap); err != nil {
			return err
		}
		if snap.IsOwner(actor) || len(snap.bots) == 0 {
			return nil
		}
		after := newSnapshot(s.now())
		if err := after.read(ctx, tx); err != nil {
			return fmt.Errorf("load permissions: %w", err)
		}
		return checkBotGrowth(snap, after, actor)
	})
}

var (
	errBotElevation = httpx.ErrForbidden("only the Owner can give a bot administrator or admin-area permissions, anywhere")
	errBotGrowth    = httpx.ErrForbidden("the change would give a bot a permission you do not have")
)

// checkBotGrowth is the bot invariant of every permission change by someone
// other than the Owner: comparing where each bot stands before and after the
// change, nothing may have been added to a bot (server-wide or in any channel)
// that is elevated (Administrator or an admin-area permission), or that the
// actor does not hold there themselves after the change. Whatever the path
// (roles, role bits, positions, memberships, overwrites, a removed deny, a
// channel moving), a bot never ends up with more than its manager has.
func checkBotGrowth(before, after *Snapshot, actor Subject) error {
	for id := range after.bots {
		bot := Subject{ID: id}
		if err := growth(before.Global(bot), after.Global(bot), after.Global(actor)); err != nil {
			return err
		}
		now := after.ChannelPermissions(bot)
		was := before.ChannelPermissions(bot)
		for ch, p := range now {
			if err := growth(was[ch], p, after.Channel(actor, ch)); err != nil {
				return err
			}
		}
	}
	return nil
}

// growth checks what was added between before and after against what the
// actor holds.
func growth(before, after, actor Set) error {
	added := after &^ before
	switch {
	case added.HasAny(Elevated):
		return errBotElevation
	case added&^actor != 0:
		return errBotGrowth
	}
	return nil
}

func (s *Service) mutate(ctx context.Context, actor Subject, need Set, fn func(tx pgx.Tx, snap *Snapshot) ([]uuid.UUID, error)) error {
	var affected []uuid.UUID
	err := s.Locked(ctx, actor, func(tx pgx.Tx, snap *Snapshot) error {
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
	err := row.Scan(&r.ID, &r.Name, &r.Color, &r.Position, &perms, &r.Hoist, &r.Mentionable, &r.Everyone, &r.MaxUploadMB)
	r.Permissions = Set(perms)
	return r, err
}

const roleColumns = `id, name, color, position, permissions, hoist, mentionable, is_everyone, max_upload_mb`

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
		if _, err := tx.Exec(ctx, `UPDATE roles SET position = position + 1 WHERE NOT is_everyone`); err != nil {
			return nil, fmt.Errorf("shift roles: %w", err)
		}
		created, err = scanRole(tx.QueryRow(ctx, `
			INSERT INTO roles (name, color, position, permissions, hoist, mentionable, max_upload_mb)
			VALUES ($1, $2, 1, $3, $4, $5, $6) RETURNING `+roleColumns,
			name, color, int64(perms), in.Hoist != nil && *in.Hoist, in.Mentionable != nil && *in.Mentionable, maxUpload))
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
	return s.setRoles(ctx, actor, ManageRoles, userID, roleIDs, false)
}

// SetBotRoles replaces the roles of a bot account. It needs Manage bots (not
// Manage roles), follows the same hierarchy rules as SetMemberRoles, and a
// role that carries Administrator or opens any part of the admin area can only
// be given by the Owner: a bot never gets such power by accident.
func (s *Service) SetBotRoles(ctx context.Context, actor Subject, userID uuid.UUID, roleIDs []uuid.UUID) error {
	return s.setRoles(ctx, actor, ManageBots, userID, roleIDs, true)
}

func (s *Service) setRoles(ctx context.Context, actor Subject, need Set, userID uuid.UUID, roleIDs []uuid.UUID, bot bool) error {
	return s.mutate(ctx, actor, need, func(tx pgx.Tx, snap *Snapshot) ([]uuid.UUID, error) {
		var role string
		var isBot bool
		err := tx.QueryRow(ctx, `SELECT role, is_bot FROM users WHERE id = $1`, userID).Scan(&role, &isBot)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, httpx.ErrNotFound("user not found")
		}
		if err != nil {
			return nil, fmt.Errorf("load user: %w", err)
		}
		if isBot != bot {
			if bot {
				return nil, httpx.ErrNotFound("user not found")
			}
			return nil, httpx.ErrForbidden("a bot's roles are set in its bot settings")
		}
		target := Subject{ID: userID, Owner: role == "admin"}
		if err := snap.CanModerate(actor, target); err != nil {
			return nil, err
		}
		// Nobody hands out roles to themselves: that would be a way to take
		// permissions they were never given. (The Owner has no roles.)
		if actor.ID == userID {
			return nil, httpx.ErrForbidden("you cannot change your own roles")
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
				// A role carries its permissions to the member: only roles
				// whose permissions the actor holds themselves can be given.
				if err := checkGrant(snap, actor, snap.roles[id].Permissions); err != nil {
					return nil, err
				}
				if bot && (snap.roles[id].Permissions.HasAny(Elevated) || snap.RoleGrantsElevatedInChannels(id)) && !snap.IsOwner(actor) {
					return nil, httpx.ErrForbidden("only the Owner can give a bot a role with administrator or admin-area permissions")
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
			// Hidden channels inside would be affected without the actor seeing them.
			if !snap.CategoryVisible(actor, id) || !snap.CanSeeAllChannelsIn(actor, id) {
				return nil, httpx.ErrNotFound("category not found")
			}
			held = snap.CategoryHeld(actor, id)
			existing = snap.categoryOv[id]
		}
		type key struct {
			typ string
			id  uuid.UUID
		}
		dup := map[key]bool{}
		next := map[key]overwrite{}
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
			switch o.Type {
			case "role":
				if _, ok := snap.roles[o.ID]; !ok {
					return nil, httpx.ErrInvalidInput("unknown role")
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
			default:
				return nil, httpx.ErrInvalidInput("type must be role or member")
			}
			if o.Allow != 0 || o.Deny != 0 {
				next[k] = overwrite{allow: o.Allow, deny: o.Deny}
			}
		}
		// Whatever changes (an entry added, changed or removed) needs the
		// actor's authority over its subject and over the permissions that
		// change, allowed or denied.
		old := map[key]overwrite{}
		if existing != nil {
			for rid, ow := range existing.roles {
				old[key{"role", rid}] = ow
			}
			for uid, ow := range existing.members {
				old[key{"member", uid}] = ow
			}
		}
		changed := map[key]Set{}
		for k, ow := range next {
			if prev := old[k]; prev != ow {
				changed[k] = (prev.allow ^ ow.allow) | (prev.deny ^ ow.deny)
			}
		}
		for k, prev := range old {
			if _, kept := next[k]; !kept {
				changed[k] = prev.allow | prev.deny
			}
		}
		for k, bits := range changed {
			if k.typ == "role" {
				// @everyone sits at position 0 and is open to anyone who may
				// manage this channel; every other role must be below the actor.
				if role := snap.roles[k.id]; !role.Everyone {
					if err := snap.CanManageRole(actor, role); err != nil {
						return nil, err
					}
				}
			} else {
				// Nobody lifts a restriction from themselves (or sets one up
				// to bargain with); that is for someone above them.
				if k.id == actor.ID && !snap.IsOwner(actor) {
					return nil, httpx.ErrForbidden("you cannot change your own overwrite")
				}
				if err := snap.CanModerate(actor, Subject{ID: k.id}); err != nil {
					return nil, err
				}
			}
			if name := held.MissingName(bits); name != "" {
				return nil, httpx.ErrForbidden("cannot change a permission you do not have: " + name)
			}
			// Elevated channel permissions (Disconnect members) for a bot are
			// the Owner's to give, as are administrator roles.
			if added := next[k].allow &^ old[k].allow; added.HasAny(Elevated) && !snap.IsOwner(actor) &&
				(k.typ == "member" && snap.bots[k.id] || k.typ == "role" && snap.RoleHeldByBot(k.id)) {
				return nil, httpx.ErrForbidden("only the Owner can allow a bot administrator or admin-area permissions")
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
