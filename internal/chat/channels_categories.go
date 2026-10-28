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

// DeleteCategory removes a category; its channels become uncategorized.
func DeleteCategory(ctx context.Context, p *db.Pool, categoryID uuid.UUID) error {
	return pgx.BeginFunc(ctx, p, func(tx pgx.Tx) error { return DeleteCategoryTx(ctx, tx, categoryID) })
}

func checkCategoryMergeable(ctx context.Context, tx pgx.Tx, categoryID uuid.UUID) error {
	var inexact bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM permission_overwrites WHERE category_id = $1)
		   AND EXISTS (SELECT 1 FROM permission_overwrites o JOIN channels ch ON ch.id = o.channel_id WHERE ch.category_id = $1)`,
		categoryID).Scan(&inexact); err != nil {
		return fmt.Errorf("check category permissions: %w", err)
	}
	if inexact {
		return httpx.ErrConflict("clear the permissions of the channels in this category, or of the category, first: they cannot be merged exactly")
	}
	return nil
}

func copyCategoryOverwrites(ctx context.Context, tx pgx.Tx, categoryID uuid.UUID) error {
	// Each overwrite names a role or a member (a table constraint), so one
	// statement copies both kinds.
	if _, err := tx.Exec(ctx, `
		INSERT INTO permission_overwrites (channel_id, role_id, user_id, allow, deny)
		SELECT ch.id, co.role_id, co.user_id, co.allow, co.deny
		FROM channels ch JOIN permission_overwrites co ON co.category_id = ch.category_id
		WHERE ch.category_id = $1`, categoryID); err != nil {
		return fmt.Errorf("keep category permissions: %w", err)
	}
	return nil
}

// DeleteCategoryTx is DeleteCategory inside a transaction. The category's
// permission overwrites do not vanish for its channels: they are copied to
// each channel first, so deleting a private category never makes its channels
// public. That copy is only exact when the channels have no overwrites of
// their own (two layers do not always flatten into one); otherwise the
// deletion is refused with 409 until one side is cleared.
func DeleteCategoryTx(ctx context.Context, tx pgx.Tx, categoryID uuid.UUID) error {
	var one int
	err := tx.QueryRow(ctx, `SELECT 1 FROM categories WHERE id = $1 FOR UPDATE`, categoryID).Scan(&one)
	if errors.Is(err, pgx.ErrNoRows) {
		return errCategoryNotFound
	}
	if err != nil {
		return fmt.Errorf("lock category: %w", err)
	}
	if err := checkCategoryMergeable(ctx, tx, categoryID); err != nil {
		return err
	}
	if err := copyCategoryOverwrites(ctx, tx, categoryID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM categories WHERE id = $1`, categoryID); err != nil {
		return fmt.Errorf("delete category: %w", err)
	}
	return nil
}

var errCategoryNotFound = httpx.ErrNotFound("category not found")
