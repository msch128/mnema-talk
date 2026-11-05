package account

import (
	"context"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/userdata"
)

// capped is a dataset of an export and whether it was cut at exportMaxRows.
// The archive lists every cut dataset in truncated.json, so nothing is left
// out silently.
type capped struct {
	value     any
	truncated bool
}

// fetchRows runs a query and returns at most limit rows. The query ends in a
// LIMIT whose placeholder is numbered after args; one extra row is asked for
// to learn whether the list was cut.
func fetchRows[T any](ctx context.Context, q db.Querier, limit int, sql string, args ...any) ([]T, bool, error) {
	return userdata.FetchRows[T](ctx, q, limit, sql, args...)
}

// listOf turns typed rows into the list of a dataset (an empty one is [], not
// null).
func listOf[T any](rows []T, truncated bool) capped {
	return capped{value: userdata.AsList(rows), truncated: truncated}
}

// listDataset is the whole of a dataset that needs no further work: the
// person's rows, capped at exportMaxRows. The query takes the person as $1 and
// ends in LIMIT $2.
func listDataset[T any](ctx context.Context, q db.Querier, user uuid.UUID, sql string) (capped, error) {
	rows, cut, err := fetchRows[T](ctx, q, exportMaxRows, sql, user)
	if err != nil {
		return capped{}, err
	}
	return listOf(rows, cut), nil
}
