package perm

import (
	"context"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// ReorderRoles sets the hierarchy: ids lists every custom role, highest
// first. A member can only move roles that are below their own highest role
// and only to places below it; roles at or above it keep their position.
func (s *Service) ReorderRoles(ctx context.Context, actor Subject, ids []uuid.UUID) error {
	return s.mutate(ctx, actor, ManageRoles, func(tx pgx.Tx, snap *Snapshot) ([]uuid.UUID, error) {
		ids, custom, err := completeRoleOrder(snap, ids)
		if err != nil {
			return nil, err
		}
		return nil, moveRoles(ctx, tx, snap, actor, ids, custom)
	})
}

// roleMove is one role and the position it moves to.
type roleMove struct {
	id  uuid.UUID
	pos int
}

// moveRoles gives each role the position its place in ids implies.
func moveRoles(ctx context.Context, tx pgx.Tx, snap *Snapshot, actor Subject, ids []uuid.UUID, custom int) error {
	moves, err := planRoleMoves(snap, actor, ids, custom)
	if err != nil {
		return err
	}
	return applyRoleMoves(ctx, tx, moves)
}

func planRoleMoves(snap *Snapshot, actor Subject, ids []uuid.UUID, custom int) ([]roleMove, error) {
	seen := make(map[uuid.UUID]bool, len(ids))
	top := snap.TopPosition(actor)
	var moves []roleMove
	for i, id := range ids {
		role, ok := snap.roles[id]
		if isInvalidRoleOrderTarget(ok, role.Everyone, seen[id]) {
			return nil, httpx.ErrInvalidInput("list every role exactly once")
		}
		seen[id] = true
		pos := custom - i
		if pos == role.Position {
			continue
		}
		if !canMoveRole(snap, actor, role.Position, pos, top) {
			return nil, httpx.ErrForbidden("roles at or above your highest role cannot be moved")
		}
		moves = append(moves, roleMove{id: id, pos: pos})
	}
	return moves, nil
}

func isInvalidRoleOrderTarget(ok bool, everyone bool, seen bool) bool {
	if !ok || everyone {
		return true
	}
	return seen
}

func canMoveRole(snap *Snapshot, actor Subject, oldPos, newPos, top int) bool {
	if snap.IsOwner(actor) {
		return true
	}
	return oldPos < top && newPos < top
}

func applyRoleMoves(ctx context.Context, tx pgx.Tx, moves []roleMove) error {
	if len(moves) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, len(moves))
	positions := make([]int, len(moves))
	for i, m := range moves {
		ids[i] = m.id
		positions[i] = m.pos
	}
	const q = `UPDATE roles SET position = v.pos, updated_at = NOW()
FROM (SELECT UNNEST($1::uuid[]) AS id, UNNEST($2::int[]) AS pos) v
WHERE roles.id = v.id`
	if _, err := tx.Exec(ctx, q, ids, positions); err != nil {
		return fmt.Errorf("move role: %w", err)
	}
	return nil
}

// completeRoleOrder normalises a submitted order and returns it with the
// number of roles people arrange. A bot's own role is not part of that
// hierarchy: it stays in the managed block at the bottom whatever the list
// says. A list written before the Bot role existed leaves it out: it goes to
// the bottom, where it started.
func completeRoleOrder(snap *Snapshot, ids []uuid.UUID) ([]uuid.UUID, int, error) {
	custom := countCustomRoles(snap.roles)
	ids = slices.DeleteFunc(slices.Clone(ids), func(id uuid.UUID) bool { return snap.roles[id].BotUserID != nil })
	ids = appendMissingManaged(snap.roles, ids)
	if len(ids) != custom {
		return nil, 0, httpx.ErrInvalidInput("list every role exactly once")
	}
	return ids, custom, nil
}

func countCustomRoles(roles map[uuid.UUID]Role) int {
	n := 0
	for _, r := range roles {
		if isCustomOrderRole(r) {
			n++
		}
	}
	return n
}

func isCustomOrderRole(r Role) bool {
	return !r.Everyone && r.BotUserID == nil
}

func appendMissingManaged(roles map[uuid.UUID]Role, ids []uuid.UUID) []uuid.UUID {
	for id, r := range roles {
		if !r.Managed || r.BotUserID != nil {
			continue
		}
		if !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	return ids
}

// Roles lists every role, highest first.
func (s *Service) Roles(ctx context.Context) ([]Role, error) {
	snap, err := s.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	return snap.Roles(), nil
}
