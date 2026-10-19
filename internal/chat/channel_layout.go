package chat

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// CategoryOrder and ChannelPlacement describe a drag-and-drop result.
type CategoryOrder struct {
	ID        uuid.UUID `json:"id" format:"uuid"`
	SortOrder int       `json:"sort_order"`
}

type ChannelPlacement struct {
	ID         uuid.UUID  `json:"id" format:"uuid"`
	CategoryID *uuid.UUID `json:"category_id" format:"uuid" extensions:"x-nullable"`
	SortOrder  int        `json:"sort_order"`
}

// ApplyLayout stores category order and channel placement in one
// transaction; an unknown category or channel rejects the whole change.
func ApplyLayout(ctx context.Context, p *db.Pool, cats []CategoryOrder, chans []ChannelPlacement) error {
	return pgx.BeginFunc(ctx, p, func(tx pgx.Tx) error { return ApplyLayoutTx(ctx, tx, cats, chans) })
}

// ApplyLayoutTx is ApplyLayout inside a transaction.
func ApplyLayoutTx(ctx context.Context, tx pgx.Tx, cats []CategoryOrder, chans []ChannelPlacement) error {
	if err := lockChannelLayout(ctx, tx); err != nil {
		return err
	}
	for _, c := range cats {
		tag, err := tx.Exec(ctx, `UPDATE categories SET sort_order = $2 WHERE id = $1`, c.ID, c.SortOrder)
		if err != nil {
			return fmt.Errorf("order category: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return errCategoryNotFound
		}
	}
	for _, c := range chans {
		tag, err := tx.Exec(ctx, `
			UPDATE channels SET category_id = $2, sort_order = $3
			WHERE id = $1 AND ($2::uuid IS NULL OR EXISTS (SELECT 1 FROM categories WHERE id = $2))`,
			c.ID, c.CategoryID, c.SortOrder)
		if err != nil {
			return fmt.Errorf("place channel: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return httpx.ErrNotFound("channel or category not found")
		}
	}
	return nil
}

// channelLayoutLockID is the transaction-scoped advisory lock that serialises
// changes to channel placement (layout drags and duplications), so they never
// interleave and cannot deadlock on each other's row locks.
const channelLayoutLockID = 0x6d6e6c6179

func lockChannelLayout(ctx context.Context, tx pgx.Tx) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, channelLayoutLockID); err != nil {
		return fmt.Errorf("lock channel layout: %w", err)
	}
	return nil
}

// DuplicateChannel copies a channel's own properties (name, type, topic,
// category) into a new channel placed directly below it in the same group
// (its category, or the uncategorized channels). Messages, reactions and
// per-user state (read position, notification level) are not copied. Channel
// names need not be unique, so the copy keeps the source name unchanged.
func DuplicateChannel(ctx context.Context, p *db.Pool, sourceID uuid.UUID) (*Channel, error) {
	var ch Channel
	err := pgx.BeginFunc(ctx, p, func(tx pgx.Tx) error {
		if err := lockChannelLayout(ctx, tx); err != nil {
			return err
		}
		var src Channel
		err := tx.QueryRow(ctx, `
			SELECT id, category_id, name, type, topic, sort_order, user_limit, slow_mode_seconds
			FROM channels WHERE id = $1 FOR UPDATE`, sourceID).
			Scan(&src.ID, &src.CategoryID, &src.Name, &src.Type, &src.Topic, &src.SortOrder, &src.UserLimit, &src.SlowModeSeconds)
		if errors.Is(err, pgx.ErrNoRows) {
			return errChannelNotFound
		}
		if err != nil {
			return fmt.Errorf("lock channel: %w", err)
		}

		// The source's group, in the order GET /api/channels lists it.
		ids, orders, err := lockChannelGroup(ctx, tx, src.CategoryID)
		if err != nil {
			return err
		}
		srcIdx := -1
		for i, id := range ids {
			if id == src.ID {
				srcIdx = i
				break
			}
		}
		if srcIdx < 0 { // cannot happen: the source row is locked
			return errChannelNotFound
		}

		newOrder, shifted := placeBelow(orders, srcIdx)
		batch := &pgx.Batch{}
		for i, order := range shifted {
			batch.Queue(`UPDATE channels SET sort_order = $2 WHERE id = $1`, ids[srcIdx+1+i], order)
		}
		if batch.Len() > 0 {
			if err := tx.SendBatch(ctx, batch).Close(); err != nil {
				return fmt.Errorf("shift channels: %w", err)
			}
		}

		err = tx.QueryRow(ctx, `
			INSERT INTO channels (category_id, name, type, topic, sort_order, user_limit, slow_mode_seconds) VALUES ($1, $2, $3, $4, $5, $6, $7)
			RETURNING id, number, category_id, name, type, topic, sort_order, created_at, user_limit, slow_mode_seconds`,
			src.CategoryID, src.Name, src.Type, src.Topic, newOrder, src.UserLimit, src.SlowModeSeconds).
			Scan(&ch.ID, &ch.Number, &ch.CategoryID, &ch.Name, &ch.Type, &ch.Topic, &ch.SortOrder, &ch.CreatedAt, &ch.UserLimit, &ch.SlowModeSeconds)
		if err != nil {
			return fmt.Errorf("duplicate channel: %w", err)
		}
		// A copy of a private channel stays private.
		if _, err := tx.Exec(ctx, `
			INSERT INTO permission_overwrites (channel_id, role_id, user_id, allow, deny)
			SELECT $1, role_id, user_id, allow, deny FROM permission_overwrites WHERE channel_id = $2`,
			ch.ID, src.ID); err != nil {
			return fmt.Errorf("copy permissions: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &ch, nil
}

// lockChannelGroup locks the channels of one category (nil: the
// uncategorized ones) and returns their IDs and sort orders in display order.
func lockChannelGroup(ctx context.Context, tx pgx.Tx, categoryID *uuid.UUID) ([]uuid.UUID, []int, error) {
	rows, err := tx.Query(ctx, `
		SELECT id, sort_order FROM channels
		WHERE category_id IS NOT DISTINCT FROM $1
		ORDER BY sort_order, created_at, id
		FOR UPDATE`, categoryID)
	if err != nil {
		return nil, nil, fmt.Errorf("lock channel group: %w", err)
	}
	defer rows.Close()
	var ids []uuid.UUID
	var orders []int
	for rows.Next() {
		var id uuid.UUID
		var order int
		if err := rows.Scan(&id, &order); err != nil {
			return nil, nil, err
		}
		ids = append(ids, id)
		orders = append(orders, order)
	}
	return ids, orders, rows.Err()
}

// placeBelow computes the sort order of a channel inserted directly after
// orders[src], where orders are a group's sort orders in display order
// (sort_order, then created_at). It returns the new channel's sort order and
// the new sort orders of every channel after src: each moves down by one, and
// further where needed so it still sorts strictly after its predecessor (a
// tie would be broken by created_at, putting an older channel above the copy).
func placeBelow(orders []int, src int) (int, []int) {
	newOrder := orders[src] + 1
	shifted := make([]int, 0, len(orders)-src-1)
	prev := newOrder
	for _, o := range orders[src+1:] {
		next := max(o+1, prev+1)
		shifted = append(shifted, next)
		prev = next
	}
	return newOrder, shifted
}
