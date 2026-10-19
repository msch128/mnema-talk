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

// moveRoles gives each role the position its place in ids implies.
func moveRoles(ctx context.Context, tx pgx.Tx, snap *Snapshot, actor Subject, ids []uuid.UUID, custom int) error {
	seen := map[uuid.UUID]bool{}
	top := snap.TopPosition(actor)
	for i, id := range ids {
		role, ok := snap.roles[id]
		if !ok || role.Everyone || seen[id] {
			return httpx.ErrInvalidInput("list every role exactly once")
		}
		seen[id] = true
		pos := custom - i
		if pos == role.Position {
			continue
		}
		if !snap.IsOwner(actor) && (role.Position >= top || pos >= top) {
			return httpx.ErrForbidden("roles at or above your highest role cannot be moved")
		}
		if _, err := tx.Exec(ctx, `UPDATE roles SET position = $2, updated_at = NOW() WHERE id = $1`, id, pos); err != nil {
			return fmt.Errorf("move role: %w", err)
		}
	}
	return nil
}

// completeRoleOrder normalises a submitted order and returns it with the
// number of roles people arrange. A bot's own role is not part of that
// hierarchy: it stays in the managed block at the bottom whatever the list
// says. A list written before the Bot role existed leaves it out: it goes to
// the bottom, where it started.
func completeRoleOrder(snap *Snapshot, ids []uuid.UUID) ([]uuid.UUID, int, error) {
	custom := 0
	for _, r := range snap.roles {
		if !r.Everyone && r.BotUserID == nil {
			custom++
		}
	}
	ids = slices.DeleteFunc(slices.Clone(ids), func(id uuid.UUID) bool { return snap.roles[id].BotUserID != nil })
	for id, r := range snap.roles {
		if r.Managed && r.BotUserID == nil && !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	if len(ids) != custom {
		return nil, 0, httpx.ErrInvalidInput("list every role exactly once")
	}
	return ids, custom, nil
}

// Roles lists every role, highest first.
func (s *Service) Roles(ctx context.Context) ([]Role, error) {
	snap, err := s.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	return snap.Roles(), nil
}
