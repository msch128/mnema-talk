package perm

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/httpx"
)

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
		return changeRoles(ctx, tx, snap, actor, userID, roleIDs, bot)
	})
}

// changeRoles checks the actor's authority over the member (or bot) and over
// each role that changes, then stores the new set. It returns whose
// permissions changed.
func changeRoles(ctx context.Context, tx pgx.Tx, snap *Snapshot, actor Subject, userID uuid.UUID, roleIDs []uuid.UUID, bot bool) ([]uuid.UUID, error) {
	target, err := loadRoleTarget(ctx, tx, userID, bot)
	if err != nil {
		return nil, err
	}
	if err := snap.CanModerate(actor, target); err != nil {
		return nil, err
	}
	// Nobody hands out roles to themselves: that would be a way to take
	// permissions they were never given. (The Owner has no roles.)
	if actor.ID == userID {
		return nil, httpx.ErrForbidden("you cannot change your own roles")
	}
	want, err := wantedRoles(snap, userID, roleIDs, bot)
	if err != nil {
		return nil, err
	}
	if err := syncRoles(ctx, tx, snap, actor, userID, want, bot); err != nil {
		return nil, err
	}
	return []uuid.UUID{userID}, nil
}

// syncRoles adds the wanted roles the user lacks and removes the ones they
// no longer should hold. Every addition is authorized first; the rows then
// change in at most two statements, however many roles are involved.
func syncRoles(ctx context.Context, tx pgx.Tx, snap *Snapshot, actor Subject, userID uuid.UUID, want map[uuid.UUID]bool, bot bool) error {
	have := map[uuid.UUID]bool{}
	for _, id := range snap.userRoles[userID] {
		have[id] = true
	}
	var add, remove []uuid.UUID
	for id := range want {
		if !have[id] {
			if err := authorizeGrant(snap, actor, snap.roles[id], bot); err != nil {
				return err
			}
			add = append(add, id)
		}
	}
	// CanModerate (checked by the caller) guarantees every role the target
	// holds is below the actor's highest role, so removing any is allowed.
	for id := range have {
		if !want[id] {
			remove = append(remove, id)
		}
	}
	if err := grantRoles(ctx, tx, userID, add); err != nil {
		return err
	}
	return revokeRoles(ctx, tx, userID, remove)
}

func revokeRoles(ctx context.Context, tx pgx.Tx, userID uuid.UUID, roleIDs []uuid.UUID) error {
	if len(roleIDs) == 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, `DELETE FROM user_roles WHERE user_id = $1 AND role_id = ANY($2::uuid[])`, userID, roleIDs); err != nil {
		return fmt.Errorf("remove roles: %w", err)
	}
	return nil
}

func grantRoles(ctx context.Context, tx pgx.Tx, userID uuid.UUID, roleIDs []uuid.UUID) error {
	if len(roleIDs) == 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, `INSERT INTO user_roles (user_id, role_id) SELECT $1, unnest($2::uuid[]) ON CONFLICT DO NOTHING`, userID, roleIDs); err != nil {
		return fmt.Errorf("assign roles: %w", err)
	}
	return nil
}

// loadRoleTarget loads the account whose roles change and checks that it is
// of the kind (member or bot) the caller is editing.
func loadRoleTarget(ctx context.Context, tx pgx.Tx, userID uuid.UUID, bot bool) (Subject, error) {
	var role string
	var isBot bool
	err := tx.QueryRow(ctx, `SELECT role, is_bot FROM users WHERE id = $1`, userID).Scan(&role, &isBot)
	if errors.Is(err, pgx.ErrNoRows) {
		return Subject{}, httpx.ErrNotFound("user not found")
	}
	if err != nil {
		return Subject{}, fmt.Errorf("load user: %w", err)
	}
	if isBot != bot {
		if bot {
			return Subject{}, httpx.ErrNotFound("user not found")
		}
		return Subject{}, httpx.ErrForbidden("a bot's roles are set in its bot settings")
	}
	return Subject{ID: userID, Owner: role == "admin"}, nil
}

// wantedRoles validates the requested role IDs and returns them as a set.
func wantedRoles(snap *Snapshot, userID uuid.UUID, roleIDs []uuid.UUID, bot bool) (map[uuid.UUID]bool, error) {
	want := map[uuid.UUID]bool{}
	for _, id := range roleIDs {
		if err := checkAssignable(snap, id, userID, bot); err != nil {
			return nil, err
		}
		want[id] = true
	}
	if bot {
		// Every bot keeps the Bot role and its own role, whatever the list
		// says: they cannot be taken away, so a list without them leaves
		// them in place.
		for id, r := range snap.roles {
			if r.Managed && (r.BotUserID == nil || *r.BotUserID == userID) {
				want[id] = true
			}
		}
	}
	return want, nil
}

// checkAssignable rejects roles that cannot be requested for this account:
// unknown ones, @everyone, managed roles for members, other bots' own roles.
func checkAssignable(snap *Snapshot, roleID, userID uuid.UUID, bot bool) error {
	r, ok := snap.roles[roleID]
	switch {
	case !ok || r.Everyone:
		return httpx.ErrInvalidInput("unknown role")
	case r.Managed && !bot:
		return errManagedHeld
	case r.BotUserID != nil && *r.BotUserID != userID:
		return errOwnRole
	}
	return nil
}

// authorizeGrant checks that the actor may give the role to a member (or bot).
func authorizeGrant(snap *Snapshot, actor Subject, role Role, bot bool) error {
	if err := snap.CanManageRole(actor, role); err != nil {
		return err
	}
	// A role carries its permissions to the member: only roles
	// whose permissions the actor holds themselves can be given.
	if err := checkGrant(snap, actor, role.Permissions); err != nil {
		return err
	}
	if forbidsBotElevation(snap, actor, role, bot) {
		return httpx.ErrForbidden("only the Owner can give a bot a role with administrator or admin-area permissions")
	}
	return nil
}

func forbidsBotElevation(snap *Snapshot, actor Subject, role Role, bot bool) bool {
	if !bot || snap.IsOwner(actor) {
		return false
	}
	return role.Permissions.HasAny(Elevated) || snap.RoleGrantsElevatedInChannels(role.ID)
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
