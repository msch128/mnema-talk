package perm

import (
	"context"
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
	if err := s.requireTarget(ctx, kind, id); err != nil {
		return nil, err
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

// requireTarget checks that the category or channel exists, reloading the
// snapshot once when it is not known yet.
func (s *Service) requireTarget(ctx context.Context, kind string, id uuid.UUID) error {
	snap, err := s.Snapshot(ctx)
	if err != nil {
		return err
	}
	if knowsTarget(snap, kind, id) {
		return nil
	}
	snap, err = s.reload(ctx, true)
	if err != nil {
		return err
	}
	if !knowsTarget(snap, kind, id) {
		return httpx.ErrNotFound(kind + " not found")
	}
	return nil
}

func knowsTarget(snap *Snapshot, kind string, id uuid.UUID) bool {
	if kind == TargetChannel {
		return snap.KnowsChannel(id)
	}
	return snap.KnowsCategory(id)
}

// SetOverwrites replaces the overwrites of a category or channel. The actor
// needs ManageChannels and must see the target; they can only allow what
// they hold there themselves, and the Owner cannot be the subject of one.
func (s *Service) SetOverwrites(ctx context.Context, actor Subject, kind string, id uuid.UUID, list []Overwrite) error {
	_, ok := targetColumns[kind]
	if !ok {
		return httpx.ErrInvalidInput("unknown target")
	}
	if len(list) > maxOverwrites {
		return httpx.ErrInvalidInput("too many overwrites")
	}
	return s.mutate(ctx, actor, ManageChannels, func(tx pgx.Tx, snap *Snapshot) ([]uuid.UUID, error) {
		held, existing, err := overwriteTarget(snap, actor, kind, id)
		if err != nil {
			return nil, err
		}
		if err := refuseTempTalk(ctx, tx, kind, id); err != nil {
			return nil, err
		}
		next, err := validateOverwrites(ctx, tx, snap, list)
		if err != nil {
			return nil, err
		}
		if err := checkOverwritesOfOpenRoles(snap, next); err != nil {
			return nil, err
		}
		if err := authorizeOverwriteChanges(snap, actor, held, diffOverwrites(existing, next)); err != nil {
			return nil, err
		}
		return nil, replaceOverwrites(ctx, tx, kind, id, list)
	})
}

// overwriteTarget returns what the actor holds on the category or channel and
// the overwrites it has now. A target the actor cannot see does not exist for them.
func overwriteTarget(snap *Snapshot, actor Subject, kind string, id uuid.UUID) (Set, *scope, error) {
	if kind == TargetChannel {
		if !snap.KnowsChannel(id) {
			return 0, nil, httpx.ErrNotFound("channel not found")
		}
		held := snap.Channel(actor, id)
		if !held.Has(ViewChannel) {
			return 0, nil, ErrChannelHidden
		}
		return held, snap.channelOv[id], nil
	}
	if !snap.KnowsCategory(id) {
		return 0, nil, httpx.ErrNotFound("category not found")
	}
	// Hidden channels inside would be affected without the actor seeing them.
	if !snap.CategoryVisible(actor, id) || !snap.CanSeeAllChannelsIn(actor, id) {
		return 0, nil, httpx.ErrNotFound("category not found")
	}
	return snap.CategoryHeld(actor, id), snap.categoryOv[id], nil
}

// overwriteKey identifies the subject (role or member) of one overwrite.
type overwriteKey struct {
	typ string
	id  uuid.UUID
}

// validateOverwrites checks the submitted list and returns the entries that
// actually allow or deny something, by subject.
func validateOverwrites(ctx context.Context, tx pgx.Tx, snap *Snapshot, list []Overwrite) (map[overwriteKey]overwrite, error) {
	roles, err := memberAccountRoles(ctx, tx, list)
	if err != nil {
		return nil, err
	}
	seen := map[overwriteKey]bool{}
	next := map[overwriteKey]overwrite{}
	for _, o := range list {
		k := overwriteKey{o.Type, o.ID}
		if seen[k] {
			return nil, httpx.ErrInvalidInput("duplicate overwrite")
		}
		seen[k] = true
		if err := overwriteBitsRejected(o); err != nil {
			return nil, err
		}
		if err := checkOverwriteSubject(snap, roles, o); err != nil {
			return nil, err
		}
		if o.Allow != 0 || o.Deny != 0 {
			next[k] = overwrite{allow: o.Allow, deny: o.Deny}
		}
	}
	return next, nil
}

func overwriteBitsRejected(o Overwrite) error {
	if (o.Allow|o.Deny)&^ChannelScoped != 0 {
		return httpx.ErrInvalidInput("these permissions cannot be overwritten per channel")
	}
	if o.Allow&o.Deny != 0 {
		return httpx.ErrInvalidInput("a permission cannot be both allowed and denied")
	}
	return nil
}

// memberAccountRoles loads the account role of every member the list names,
// in one statement.
func memberAccountRoles(ctx context.Context, tx pgx.Tx, list []Overwrite) (map[uuid.UUID]string, error) {
	var ids []uuid.UUID
	for _, o := range list {
		if o.Type == "member" {
			ids = append(ids, o.ID)
		}
	}
	roles := map[uuid.UUID]string{}
	if len(ids) == 0 {
		return roles, nil
	}
	rows, err := tx.Query(ctx, `SELECT id, role FROM users WHERE id = ANY($1::uuid[])`, ids)
	if err != nil {
		return nil, fmt.Errorf("load members: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var role string
		if err := rows.Scan(&id, &role); err != nil {
			return nil, fmt.Errorf("load members: %w", err)
		}
		roles[id] = role
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("load members: %w", err)
	}
	return roles, nil
}

// checkOverwriteSubject makes sure the role or member exists and is not the
// Owner. accountRoles holds the account role of every member in the list.
func checkOverwriteSubject(snap *Snapshot, accountRoles map[uuid.UUID]string, o Overwrite) error {
	switch o.Type {
	case "role":
		if _, ok := snap.roles[o.ID]; !ok {
			return httpx.ErrInvalidInput("unknown role")
		}
	case "member":
		role, ok := accountRoles[o.ID]
		if !ok {
			return httpx.ErrInvalidInput("unknown member")
		}
		if role == "admin" || snap.owners[o.ID] {
			return ErrOwnerProtected
		}
	default:
		return httpx.ErrInvalidInput("type must be role or member")
	}
	return nil
}

// overwriteChange is one subject whose overwrite is added, changed or removed.
type overwriteChange struct {
	// bits are the permissions that change, allowed or denied.
	bits Set
	// added are the permissions that are newly allowed.
	added Set
}

// diffOverwrites compares the stored overwrites with the new ones.
func diffOverwrites(existing *scope, next map[overwriteKey]overwrite) map[overwriteKey]overwriteChange {
	old := map[overwriteKey]overwrite{}
	if existing != nil {
		for rid, ow := range existing.roles {
			old[overwriteKey{"role", rid}] = ow
		}
		for uid, ow := range existing.members {
			old[overwriteKey{"member", uid}] = ow
		}
	}
	changed := map[overwriteKey]overwriteChange{}
	for k, ow := range next {
		if prev := old[k]; prev != ow {
			changed[k] = overwriteChange{
				bits:  (prev.allow ^ ow.allow) | (prev.deny ^ ow.deny),
				added: ow.allow &^ prev.allow,
			}
		}
	}
	for k, prev := range old {
		if _, kept := next[k]; !kept {
			changed[k] = overwriteChange{bits: prev.allow | prev.deny}
		}
	}
	return changed
}

// authorizeOverwriteChanges demands the actor's authority over every changed
// subject and over the permissions that change, allowed or denied.
func authorizeOverwriteChanges(snap *Snapshot, actor Subject, held Set, changed map[overwriteKey]overwriteChange) error {
	for k, c := range changed {
		if err := authorizeOverwriteSubject(snap, actor, k); err != nil {
			return err
		}
		if name := held.MissingName(c.bits); name != "" {
			return httpx.ErrForbidden("cannot change a permission you do not have: " + name)
		}
		// Elevated channel permissions (Disconnect members) for a bot are
		// the Owner's to give, as are administrator roles.
		if botElevationRejected(snap, actor, k, c.added) {
			return httpx.ErrForbidden("only the Owner can allow a bot administrator or admin-area permissions")
		}
	}
	return nil
}

func botElevationRejected(snap *Snapshot, actor Subject, k overwriteKey, added Set) bool {
	if !added.HasAny(Elevated) || snap.IsOwner(actor) {
		return false
	}
	if k.typ == "member" && snap.bots[k.id] {
		return true
	}
	return k.typ == "role" && snap.RoleHeldByBot(k.id)
}

func authorizeOverwriteSubject(snap *Snapshot, actor Subject, k overwriteKey) error {
	if k.typ == "role" {
		// @everyone sits at position 0 and is open to anyone who may
		// manage this channel; every other role must be below the actor.
		if role := snap.roles[k.id]; !role.Everyone {
			return snap.CanManageRole(actor, role)
		}
		return nil
	}
	// Nobody lifts a restriction from themselves (or sets one up
	// to bargain with); that is for someone above them.
	if k.id == actor.ID && !snap.IsOwner(actor) {
		return httpx.ErrForbidden("you cannot change your own overwrite")
	}
	return snap.CanModerate(actor, Subject{ID: k.id})
}

// replaceOverwrites rewrites the stored overwrites of the target with the list:
// one statement clears the old ones, one inserts every non-empty new one.
func replaceOverwrites(ctx context.Context, tx pgx.Tx, kind string, id uuid.UUID, list []Overwrite) error {
	if err := rewriteOverwrites(ctx, tx, targetColumns[kind], id, list); err != nil {
		return err
	}
	if kind != TargetChannel {
		return nil
	}
	return syncTempTalks(ctx, tx, id)
}

func rewriteOverwrites(ctx context.Context, tx pgx.Tx, col string, id uuid.UUID, list []Overwrite) error {
	if _, err := tx.Exec(ctx, `DELETE FROM permission_overwrites WHERE `+col+` = $1`, id); err != nil {
		return fmt.Errorf("clear overwrites: %w", err)
	}
	var subjects []uuid.UUID
	var members []bool
	var allow, deny []int64
	for _, o := range list {
		if o.Allow == 0 && o.Deny == 0 {
			continue
		}
		subjects = append(subjects, o.ID)
		members = append(members, o.Type == "member")
		allow = append(allow, int64(o.Allow))
		deny = append(deny, int64(o.Deny))
	}
	if len(subjects) == 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO permission_overwrites (`+col+`, role_id, user_id, allow, deny)
		SELECT $1, CASE WHEN NOT m THEN s END, CASE WHEN m THEN s END, a, d
		FROM unnest($2::uuid[], $3::bool[], $4::bigint[], $5::bigint[]) AS v(s, m, a, d)`,
		id, subjects, members, allow, deny); err != nil {
		return fmt.Errorf("insert overwrites: %w", err)
	}
	return nil
}
