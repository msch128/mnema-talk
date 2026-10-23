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
	if err := orderCategories(ctx, tx, cats); err != nil {
		return err
	}
	return placeChannels(ctx, tx, chans)
}

// orderCategories sets every sort order in one statement. When an ID repeats,
// the last entry wins; an unknown category rejects the whole change.
func orderCategories(ctx context.Context, tx pgx.Tx, cats []CategoryOrder) error {
	last := map[uuid.UUID]int64{}
	for _, c := range cats {
		last[c.ID] = int64(c.SortOrder)
	}
	if len(last) == 0 {
		return nil
	}
	ids, orders := make([]uuid.UUID, 0, len(last)), make([]int64, 0, len(last))
	for id, order := range last {
		ids, orders = append(ids, id), append(orders, order)
	}
	tag, err := tx.Exec(ctx, `
		UPDATE categories c SET sort_order = v.sort_order
		FROM unnest($1::uuid[], $2::bigint[]) AS v(id, sort_order)
		WHERE c.id = v.id`, ids, orders)
	if err != nil {
		return fmt.Errorf("order categories: %w", err)
	}
	if tag.RowsAffected() != int64(len(ids)) {
		return errCategoryNotFound
	}
	return nil
}

// placeChannels moves every channel in one statement. When an ID repeats, the
// last entry wins; an unknown channel or category rejects the whole change.
func placeChannels(ctx context.Context, tx pgx.Tx, chans []ChannelPlacement) error {
	last := map[uuid.UUID]ChannelPlacement{}
	for _, c := range chans {
		last[c.ID] = c
	}
	if len(last) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, 0, len(last))
	parents := make([]*uuid.UUID, 0, len(last))
	orders := make([]int64, 0, len(last))
	for id, c := range last {
		ids, parents, orders = append(ids, id), append(parents, c.CategoryID), append(orders, int64(c.SortOrder))
	}
	tag, err := tx.Exec(ctx, `
		UPDATE channels ch SET category_id = v.category_id, sort_order = v.sort_order
		FROM unnest($1::uuid[], $2::uuid[], $3::bigint[]) AS v(id, category_id, sort_order)
		WHERE ch.id = v.id AND ch.type <> 'dm'
		  AND (v.category_id IS NULL OR EXISTS (SELECT 1 FROM categories WHERE id = v.category_id))`,
		ids, parents, orders)
	if err != nil {
		return fmt.Errorf("place channels: %w", err)
	}
	if tag.RowsAffected() != int64(len(ids)) {
		return httpx.ErrNotFound("channel or category not found")
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
	var ch *Channel
	err := pgx.BeginFunc(ctx, p, func(tx pgx.Tx) error {
		if err := lockChannelLayout(ctx, tx); err != nil {
			return err
		}
		src, err := lockSourceChannel(ctx, tx, sourceID)
		if err != nil {
			return err
		}
		newOrder, err := makeRoomBelow(ctx, tx, src)
		if err != nil {
			return err
		}
		ch, err = insertChannelCopy(ctx, tx, src, newOrder)
		return err
	})
	if err != nil {
		return nil, err
	}
	return ch, nil
}

func lockSourceChannel(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Channel, error) {
	var src Channel
	err := tx.QueryRow(ctx, `
		SELECT id, category_id, name, type, topic, sort_order, user_limit, slow_mode_seconds
		FROM channels WHERE id = $1 AND type <> 'dm' FOR UPDATE`, id).
		Scan(&src.ID, &src.CategoryID, &src.Name, &src.Type, &src.Topic, &src.SortOrder, &src.UserLimit, &src.SlowModeSeconds)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errChannelNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("lock channel: %w", err)
	}
	return &src, nil
}

// makeRoomBelow shifts the channels after src in its group (in the order
// GET /api/channels lists it) down and returns the sort order for the copy.
func makeRoomBelow(ctx context.Context, tx pgx.Tx, src *Channel) (int, error) {
	ids, orders, err := lockChannelGroup(ctx, tx, src.CategoryID)
	if err != nil {
		return 0, err
	}
	srcIdx := slices.Index(ids, src.ID)
	if srcIdx < 0 { // cannot happen: the source row is locked
		return 0, errChannelNotFound
	}

	newOrder, shifted := placeBelow(orders, srcIdx)
	batch := &pgx.Batch{}
	for i, order := range shifted {
		batch.Queue(`UPDATE channels SET sort_order = $2 WHERE id = $1`, ids[srcIdx+1+i], order)
	}
	if batch.Len() > 0 {
		if err := tx.SendBatch(ctx, batch).Close(); err != nil {
			return 0, fmt.Errorf("shift channels: %w", err)
		}
	}
	return newOrder, nil
}

// insertChannelCopy creates the copy of src at sortOrder, with the same
// permission overwrites: a copy of a private channel stays private.
func insertChannelCopy(ctx context.Context, tx pgx.Tx, src *Channel, sortOrder int) (*Channel, error) {
	var ch Channel
	err := tx.QueryRow(ctx, `
		INSERT INTO channels (category_id, name, type, topic, sort_order, user_limit, slow_mode_seconds) VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, number, category_id, name, type, topic, sort_order, created_at, user_limit, slow_mode_seconds`,
		src.CategoryID, src.Name, src.Type, src.Topic, sortOrder, src.UserLimit, src.SlowModeSeconds).
		Scan(&ch.ID, &ch.Number, &ch.CategoryID, &ch.Name, &ch.Type, &ch.Topic, &ch.SortOrder, &ch.CreatedAt, &ch.UserLimit, &ch.SlowModeSeconds)
	if err != nil {
		return nil, fmt.Errorf("duplicate channel: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO permission_overwrites (channel_id, role_id, user_id, allow, deny)
		SELECT $1, role_id, user_id, allow, deny FROM permission_overwrites WHERE channel_id = $2`,
		ch.ID, src.ID); err != nil {
		return nil, fmt.Errorf("copy permissions: %w", err)
	}
	return &ch, nil
}

// lockChannelGroup locks the channels of one category (nil: the
// uncategorized ones) and returns their IDs and sort orders in display order.
func lockChannelGroup(ctx context.Context, tx pgx.Tx, categoryID *uuid.UUID) ([]uuid.UUID, []int, error) {
	rows, err := tx.Query(ctx, `
		SELECT id, sort_order FROM channels
		WHERE category_id IS NOT DISTINCT FROM $1 AND type <> 'dm'
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
