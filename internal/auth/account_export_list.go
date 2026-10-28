package auth

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
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
	rows, err := collect(ctx, q, pgx.RowToStructByName[T], sql, append(args, limit+1)...)
	if err != nil {
		return nil, false, err
	}
	if len(rows) > limit {
		return rows[:limit], true, nil
	}
	return rows, false, nil
}

// listOf turns typed rows into the list of a dataset (an empty one is [], not
// null).
func listOf[T any](rows []T, truncated bool) capped {
	out := make([]any, len(rows))
	for i, r := range rows {
		out[i] = r
	}
	return capped{value: out, truncated: truncated}
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

// viewSet is the set of channels whose names the export may show.
type viewSet struct {
	all bool
	ids map[uuid.UUID]bool
}

// has reports whether the channel's name may appear.
func (v viewSet) has(id uuid.UUID) bool { return v.all || v.ids[id] }
