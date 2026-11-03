package userdata

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
)

// Collect runs a query and maps every row.
func Collect[T any](ctx context.Context, q db.Querier, fn pgx.RowToFunc[T], sql string, args ...any) ([]T, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, fn)
}

// FetchRows runs a query and returns at most limit rows. The query ends in a
// LIMIT whose placeholder is numbered after args; one extra row is asked for
// so the caller can tell that the list was cut.
func FetchRows[T any](ctx context.Context, q db.Querier, limit int, sql string, args ...any) ([]T, bool, error) {
	rows, err := Collect(ctx, q, pgx.RowToStructByName[T], sql, append(args, limit+1)...)
	if err != nil {
		return nil, false, err
	}
	if len(rows) > limit {
		return rows[:limit], true, nil
	}
	return rows, false, nil
}

// AsList is the JSON array of a dataset. An empty one is [] and not null.
func AsList[T any](rows []T) []any {
	out := make([]any, len(rows))
	for i, r := range rows {
		out[i] = r
	}
	return out
}
