package perm

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// MaxRoles is the most roles a server holds (managed roles included).
const MaxRoles = maxRoles

const (
	// lockKey serializes every permission change (pg_advisory_xact_lock), so
	// the hierarchy checks below see the state they write into.
	lockKey = 0x6d6e7065726d // "mnperm"

	maxRoles           = 250
	maxOverwrites      = 200
	maxRoleName        = 32
	maxRoleDescription = 200
)

// RoleInput is a role to create or the fields of one to change; omitted
// fields are kept (or defaulted on create).
type RoleInput struct {
	Name        *string `json:"name" minLength:"1" maxLength:"32" binding:"optional"`
	Color       *int    `json:"color" minimum:"0" maximum:"16777215" binding:"optional"`
	Permissions *Set    `json:"permissions" swaggertype:"integer" minimum:"0" binding:"optional"`
	Hoist       *bool   `json:"hoist" binding:"optional"`
	Mentionable *bool   `json:"mentionable" binding:"optional"`
	// SelfAssignable lets members add and remove the role themselves; refused
	// for @everyone, managed roles and roles with Administrator or admin-area permissions.
	SelfAssignable *bool `json:"self_assignable" binding:"optional"`
	// Description is one line shown to members choosing roles.
	Description *string `json:"description" maxLength:"200" binding:"optional"`
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
	return s.locked(ctx, actor, false, fn)
}

// LockedCreatingBot is Locked for a change that adds a bot: the bot invariant
// is checked even when no bot existed before, and a new bot counts as having
// had nothing, so what the Bot role and its overwrites give it is checked
// against the creator.
func (s *Service) LockedCreatingBot(ctx context.Context, actor Subject, fn func(tx pgx.Tx, snap *Snapshot) error) error {
	return s.locked(ctx, actor, true, fn)
}

func (s *Service) locked(ctx context.Context, actor Subject, always bool, fn func(tx pgx.Tx, snap *Snapshot) error) error {
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
		return s.verifyBotGrowth(ctx, tx, snap, actor, always)
	})
}

func skipBotGrowthCheck(snap *Snapshot, actor Subject, always bool) bool {
	if snap.IsOwner(actor) {
		return true
	}
	return len(snap.bots) == 0 && !always
}

func (s *Service) verifyBotGrowth(ctx context.Context, tx pgx.Tx, snap *Snapshot, actor Subject, always bool) error {
	if skipBotGrowthCheck(snap, actor, always) {
		return nil
	}
	after := newSnapshot(s.now())
	if err := after.read(ctx, tx); err != nil {
		return fmt.Errorf("load permissions: %w", err)
	}
	return checkBotGrowth(snap, after, actor)
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
		var wasGlobal Set
		var was map[uuid.UUID]Set
		if before.bots[id] { // a bot created by this change had nothing before
			wasGlobal, was = before.Global(bot), before.ChannelPermissions(bot)
		}
		if err := growth(wasGlobal, after.Global(bot), after.Global(actor)); err != nil {
			return err
		}
		now := after.ChannelPermissions(bot)
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
