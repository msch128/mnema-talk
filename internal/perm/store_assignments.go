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
			if r.Managed && !bot {
				return nil, errManagedHeld
			}
			if r.BotUserID != nil && *r.BotUserID != userID {
				return nil, errOwnRole
			}
			want[id] = true
		}
		have := map[uuid.UUID]bool{}
		for _, id := range snap.userRoles[userID] {
			have[id] = true
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
