package chat

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// What a member does to their own list of conversations: closing one,
// favouriting, and the list-wide settings. It is all the member's own view;
// no conversation or message changes, and nobody else is told.

// MaxFavouriteDMs is how many conversations one member may favourite.
const MaxFavouriteDMs = 25

var errFavouriteOpen = httpx.ErrConflict("A favourite stays in the list: remove it from the favourites first.")

// SetDMHidden closes (or reopens) a conversation in the member's list. A
// favourite cannot be closed. Anybody who is not in the conversation gets
// errDMNotFound.
func SetDMHidden(ctx context.Context, p *db.Pool, user, id uuid.UUID, hidden bool) error {
	// No row updated means the member is not in it, or it is a favourite
	// that was to be closed; the lookup below tells which.
	var updated bool
	err := p.QueryRow(ctx, `
		UPDATE dm_members SET hidden_at = CASE WHEN $3 THEN COALESCE(hidden_at, NOW()) ELSE NULL END
		WHERE channel_id = $1 AND user_id = $2 AND NOT ($3 AND favourite_pos IS NOT NULL)
		RETURNING TRUE`, id, user, hidden).Scan(&updated)
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	var member bool
	if err := p.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM dm_members WHERE channel_id = $1 AND user_id = $2)`, id, user).Scan(&member); err != nil {
		return err
	}
	if !member {
		return errDMNotFound
	}
	return errFavouriteOpen
}

// SetDMFavourites replaces the member's favourites with ids, in that order.
// Every id must be a conversation the member is in (anything else is
// errDMNotFound, so conversations of others cannot be probed). A favourite is
// always shown, so favouriting reopens a closed conversation. It returns the
// conversations whose state changed.
func SetDMFavourites(ctx context.Context, p *db.Pool, user uuid.UUID, ids []uuid.UUID) (changed []uuid.UUID, err error) {
	if err := checkFavourites(ids); err != nil {
		return nil, err
	}
	err = pgx.BeginFunc(ctx, p, func(tx pgx.Tx) error {
		// One member's changes are serialized, so the order stays whole.
		if _, err := tx.Exec(ctx, `SELECT 1 FROM users WHERE id = $1 FOR NO KEY UPDATE`, user); err != nil {
			return err
		}
		before, err := favouritePositions(ctx, tx, user)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE dm_members SET favourite_pos = NULL WHERE user_id = $1 AND favourite_pos IS NOT NULL`, user); err != nil {
			return err
		}
		added, err := updatedIDs(ctx, tx, `
			UPDATE dm_members dm SET favourite_pos = f.pos - 1, hidden_at = NULL
			FROM unnest($2::uuid[]) WITH ORDINALITY AS f(id, pos)
			WHERE dm.user_id = $1 AND dm.channel_id = f.id RETURNING dm.channel_id`, user, ids)
		if err != nil {
			return err
		}
		if len(added) != len(ids) {
			return errDMNotFound
		}
		// A favourite that kept its place did not change; one that moved,
		// came in or went out did.
		for id := range before {
			if !slices.Contains(ids, id) {
				changed = append(changed, id)
			}
		}
		for pos, id := range ids {
			if old, was := before[id]; !was || old != pos {
				changed = append(changed, id)
			}
		}
		return nil
	})
	return changed, err
}

// favouritePositions maps the member's favourites to their places.
func favouritePositions(ctx context.Context, tx pgx.Tx, user uuid.UUID) (map[uuid.UUID]int, error) {
	rows, err := tx.Query(ctx, `SELECT channel_id, favourite_pos FROM dm_members WHERE user_id = $1 AND favourite_pos IS NOT NULL`, user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID]int{}
	for rows.Next() {
		var id uuid.UUID
		var pos int
		if err := rows.Scan(&id, &pos); err != nil {
			return nil, err
		}
		out[id] = pos
	}
	return out, rows.Err()
}

// checkFavourites requires at most MaxFavouriteDMs distinct ids.
func checkFavourites(ids []uuid.UUID) error {
	if len(ids) > MaxFavouriteDMs {
		return httpx.ErrInvalidInput(fmt.Sprintf("at most %d favourites", MaxFavouriteDMs))
	}
	sorted := slices.Clone(ids)
	slices.SortFunc(sorted, func(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) })
	if len(slices.Compact(sorted)) != len(ids) {
		return httpx.ErrInvalidInput("ids must not repeat a conversation")
	}
	return nil
}

// updatedIDs runs an UPDATE ... RETURNING that yields conversation ids.
func updatedIDs(ctx context.Context, tx pgx.Tx, sql string, args ...any) ([]uuid.UUID, error) {
	rows, err := tx.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
}

// DMSettings are the member's settings for the list as a whole.
type DMSettings struct {
	// MuteAll silences every conversation: no push, no sound, no highlight.
	// Unread counts stay. A conversation's own notification setting overrides it.
	MuteAll bool `json:"mute_all"`
	// ListCollapsed is whether the member folded the sidebar section.
	ListCollapsed bool `json:"list_collapsed"`
}

// GetDMSettings reads the member's list settings.
func GetDMSettings(ctx context.Context, q db.Querier, user uuid.UUID) (DMSettings, error) {
	var s DMSettings
	err := q.QueryRow(ctx, `SELECT dm_mute_all, dm_list_collapsed FROM users WHERE id = $1`, user).Scan(&s.MuteAll, &s.ListCollapsed)
	return s, err
}

// SetDMSettings stores the member's list settings.
func SetDMSettings(ctx context.Context, q db.Querier, user uuid.UUID, s DMSettings) error {
	_, err := q.Exec(ctx, `UPDATE users SET dm_mute_all = $2, dm_list_collapsed = $3 WHERE id = $1`, user, s.MuteAll, s.ListCollapsed)
	return err
}
