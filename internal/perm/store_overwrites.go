package perm

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/httpx"
)

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
