package channels

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

func loadCategories(ctx context.Context, p *db.Pool) ([]core.Category, map[uuid.UUID]int, error) {
	catRows, err := p.Query(ctx, `SELECT id, name, sort_order, created_at FROM categories ORDER BY sort_order, created_at`)
	if err != nil {
		return nil, nil, fmt.Errorf("query categories: %w", err)
	}
	defer catRows.Close()
	categories := make([]core.Category, 0)
	categoryIdx := make(map[uuid.UUID]int)
	for catRows.Next() {
		cat := core.Category{Channels: make([]core.Channel, 0)}
		if err := catRows.Scan(&cat.ID, &cat.Name, &cat.SortOrder, &cat.CreatedAt); err != nil {
			return nil, nil, err
		}
		categoryIdx[cat.ID] = len(categories)
		categories = append(categories, cat)
	}
	return categories, categoryIdx, catRows.Err()
}

func CreateCategory(ctx context.Context, p *db.Pool, name string, sortOrder int) (*core.Category, error) {
	name, err := httpx.CleanText("name", name, core.MaxChannelNameLen, true)
	if err != nil {
		return nil, err
	}
	if err := CheckSortOrder(sortOrder); err != nil {
		return nil, err
	}
	cat := core.Category{Channels: make([]core.Channel, 0)}
	err = p.QueryRow(ctx, `
		INSERT INTO categories (name, sort_order) VALUES ($1, $2)
		RETURNING id, name, sort_order, created_at`, name, sortOrder).
		Scan(&cat.ID, &cat.Name, &cat.SortOrder, &cat.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("create category: %w", err)
	}
	return &cat, nil
}

// RenameCategory changes a category's name.
func RenameCategory(ctx context.Context, p *db.Pool, id uuid.UUID, name string) (string, error) {
	name, err := httpx.CleanText("name", name, core.MaxChannelNameLen, true)
	if err != nil {
		return "", err
	}
	tag, err := p.Exec(ctx, `UPDATE categories SET name = $2 WHERE id = $1`, id, name)
	if err != nil {
		return "", fmt.Errorf("rename category: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return "", core.ErrCategoryNotFound
	}
	return name, nil
}
