package discordimport

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/db"
)

// keptCategories names the created categories that hold a channel which is
// not one of ours.
func keptCategories(ctx context.Context, q db.Querier, cats, ours []uuid.UUID) ([]string, error) {
	rows, err := q.Query(ctx, `
		SELECT DISTINCT ca.name FROM categories ca JOIN channels ch ON ch.category_id = ca.id
		WHERE ca.id = ANY($1) AND NOT (ch.id = ANY($2)) ORDER BY ca.name`, cats, ours)
	if err != nil {
		return nil, fmt.Errorf("read kept categories: %w", err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}
