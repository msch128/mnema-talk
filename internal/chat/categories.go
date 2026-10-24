package chat

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

type Category struct {
	ID        uuid.UUID `json:"id" format:"uuid"`
	Name      string    `json:"name" maxLength:"64"`
	SortOrder int       `json:"sort_order"`
	Channels  []Channel `json:"channels"`
	CreatedAt time.Time `json:"created_at" format:"date-time"`
}

func loadCategories(ctx context.Context, p *db.Pool) ([]Category, map[uuid.UUID]int, error) {
	catRows, err := p.Query(ctx, `SELECT id, name, sort_order, created_at FROM categories ORDER BY sort_order, created_at`)
	if err != nil {
		return nil, nil, fmt.Errorf("query categories: %w", err)
	}
	defer catRows.Close()
	categories := make([]Category, 0)
	categoryIdx := make(map[uuid.UUID]int)
	for catRows.Next() {
		cat := Category{Channels: make([]Channel, 0)}
		if err := catRows.Scan(&cat.ID, &cat.Name, &cat.SortOrder, &cat.CreatedAt); err != nil {
			return nil, nil, err
		}
		categoryIdx[cat.ID] = len(categories)
		categories = append(categories, cat)
	}
	return categories, categoryIdx, catRows.Err()
}

func CreateCategory(ctx context.Context, p *db.Pool, name string, sortOrder int) (*Category, error) {
	name, err := httpx.CleanText("name", name, MaxChannelNameLen, true)
	if err != nil {
		return nil, err
	}
	cat := Category{Channels: make([]Channel, 0)}
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
func RenameCategory(ctx context.Context, p *db.Pool, id uuid.UUID, name string) error {
	name, err := httpx.CleanText("name", name, MaxChannelNameLen, true)
	if err != nil {
		return err
	}
	tag, err := p.Exec(ctx, `UPDATE categories SET name = $2 WHERE id = $1`, id, name)
	if err != nil {
		return fmt.Errorf("rename category: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return errCategoryNotFound
	}
	return nil
}
