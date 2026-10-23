//go:build integration

package chat

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/testutil"
	"github.com/msch128/mnema-talk/internal/testutil/stmtcount"
)

type layoutRow struct {
	Parent *uuid.UUID
	Order  int
}

type layoutWorld struct {
	p    *db.Pool
	cats []uuid.UUID
	chs  []uuid.UUID
}

func newLayoutWorld(t *testing.T, cats, chans int) layoutWorld {
	t.Helper()
	p := testutil.DB(t)
	testutil.Reset(t, p)
	w := layoutWorld{p: p}
	for i := 0; i < cats; i++ {
		c, err := CreateCategory(context.Background(), p, fmt.Sprintf("cat-%d", i), i)
		if err != nil {
			t.Fatal(err)
		}
		w.cats = append(w.cats, c.ID)
	}
	for i := 0; i < chans; i++ {
		c, err := CreateChannel(context.Background(), p, nil, fmt.Sprintf("ch-%d", i), ChannelTypeText, "", i)
		if err != nil {
			t.Fatal(err)
		}
		w.chs = append(w.chs, c.ID)
	}
	return w
}

func (w layoutWorld) categoryOrders(t *testing.T) map[uuid.UUID]int {
	t.Helper()
	rows, err := w.p.Query(context.Background(), `SELECT id, sort_order FROM categories`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[uuid.UUID]int{}
	for rows.Next() {
		var id uuid.UUID
		var order int
		if err := rows.Scan(&id, &order); err != nil {
			t.Fatal(err)
		}
		out[id] = order
	}
	return out
}

func (w layoutWorld) placements(t *testing.T) map[uuid.UUID]layoutRow {
	t.Helper()
	rows, err := w.p.Query(context.Background(), `SELECT id, category_id, sort_order FROM channels`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[uuid.UUID]layoutRow{}
	for rows.Next() {
		var id uuid.UUID
		var r layoutRow
		if err := rows.Scan(&id, &r.Parent, &r.Order); err != nil {
			t.Fatal(err)
		}
		out[id] = r
	}
	return out
}

func TestApplyLayoutStoresOrderAndPlacement(t *testing.T) {
	ctx := context.Background()
	w := newLayoutWorld(t, 3, 4)
	// Reverse the categories, move two channels into categories and one back out.
	err := ApplyLayout(ctx, w.p,
		[]CategoryOrder{{w.cats[0], 2}, {w.cats[1], 1}, {w.cats[2], 0}},
		[]ChannelPlacement{{w.chs[0], &w.cats[2], 5}, {w.chs[1], &w.cats[2], 4}, {w.chs[2], nil, 9}})
	if err != nil {
		t.Fatal(err)
	}
	if got := w.categoryOrders(t); got[w.cats[0]] != 2 || got[w.cats[1]] != 1 || got[w.cats[2]] != 0 {
		t.Fatalf("category orders: %v", got)
	}
	got := w.placements(t)
	want := map[uuid.UUID]layoutRow{
		w.chs[0]: {&w.cats[2], 5}, w.chs[1]: {&w.cats[2], 4}, w.chs[2]: {nil, 9},
		w.chs[3]: {nil, 3}, // not mentioned, so untouched
	}
	for id, row := range want {
		if g := got[id]; g.Order != row.Order || (g.Parent == nil) != (row.Parent == nil) || (row.Parent != nil && *g.Parent != *row.Parent) {
			t.Fatalf("channel %v: got %+v, want %+v", id, g, row)
		}
	}
	// Moving a channel out of its category works as well.
	if err := ApplyLayout(ctx, w.p, nil, []ChannelPlacement{{w.chs[0], nil, 1}}); err != nil {
		t.Fatal(err)
	}
	if g := w.placements(t)[w.chs[0]]; g.Parent != nil || g.Order != 1 {
		t.Fatalf("channel left in category: %+v", g)
	}
}

func TestApplyLayoutLastEntryWinsForARepeatedID(t *testing.T) {
	ctx := context.Background()
	w := newLayoutWorld(t, 1, 1)
	err := ApplyLayout(ctx, w.p,
		[]CategoryOrder{{w.cats[0], 7}, {w.cats[0], 8}},
		[]ChannelPlacement{{w.chs[0], &w.cats[0], 3}, {w.chs[0], nil, 6}})
	if err != nil {
		t.Fatal(err)
	}
	if got := w.categoryOrders(t)[w.cats[0]]; got != 8 {
		t.Fatalf("category order = %d, want 8", got)
	}
	if g := w.placements(t)[w.chs[0]]; g.Parent != nil || g.Order != 6 {
		t.Fatalf("channel = %+v, want top level at 6", g)
	}
}

func TestApplyLayoutWithNothingChangesNothing(t *testing.T) {
	w := newLayoutWorld(t, 2, 2)
	before, beforeCats := w.placements(t), w.categoryOrders(t)
	if err := ApplyLayout(context.Background(), w.p, nil, nil); err != nil {
		t.Fatal(err)
	}
	after, afterCats := w.placements(t), w.categoryOrders(t)
	for id, row := range before {
		if after[id].Order != row.Order {
			t.Fatalf("channel %v moved: %+v -> %+v", id, row, after[id])
		}
	}
	for id, order := range beforeCats {
		if afterCats[id] != order {
			t.Fatalf("category %v moved: %d -> %d", id, order, afterCats[id])
		}
	}
}

func TestApplyLayoutRejectsUnknownEntriesAsAWhole(t *testing.T) {
	ctx := context.Background()
	w := newLayoutWorld(t, 1, 2)
	ghost := uuid.New()
	cases := []struct {
		name  string
		cats  []CategoryOrder
		chans []ChannelPlacement
	}{
		{"unknown category", []CategoryOrder{{w.cats[0], 9}, {ghost, 1}}, nil},
		{"unknown channel", nil, []ChannelPlacement{{w.chs[0], nil, 9}, {ghost, nil, 1}}},
		{"unknown parent category", nil, []ChannelPlacement{{w.chs[0], nil, 9}, {w.chs[1], &ghost, 1}}},
	}
	for _, c := range cases {
		if err := ApplyLayout(ctx, w.p, c.cats, c.chans); err == nil {
			t.Fatalf("%s: accepted", c.name)
		}
		if got := w.categoryOrders(t)[w.cats[0]]; got != 0 {
			t.Fatalf("%s: category order changed to %d", c.name, got)
		}
		if got := w.placements(t)[w.chs[0]].Order; got != 0 {
			t.Fatalf("%s: channel order changed to %d", c.name, got)
		}
	}
	if err := ApplyLayout(ctx, w.p, []CategoryOrder{{ghost, 1}}, nil); !errors.Is(err, errCategoryNotFound) {
		t.Fatalf("unknown category error = %v", err)
	}
}

func TestApplyLayoutCostsTheSameStatementsForFewAndManyEntries(t *testing.T) {
	ctx := context.Background()
	w := newLayoutWorld(t, 12, 40)
	pool, counter := stmtcount.Pool(t, w.p.Pool)
	counted := &db.Pool{Pool: pool}
	measure := func(cats int, chans int) int {
		var co []CategoryOrder
		for i := 0; i < cats; i++ {
			co = append(co, CategoryOrder{w.cats[i], 100 + i})
		}
		var cp []ChannelPlacement
		for i := 0; i < chans; i++ {
			cp = append(cp, ChannelPlacement{w.chs[i], &w.cats[i%len(w.cats)], i})
		}
		counter.Reset()
		if err := ApplyLayout(ctx, counted, co, cp); err != nil {
			t.Fatal(err)
		}
		return counter.Count()
	}
	few, many := measure(1, 1), measure(12, 40)
	if few != many {
		t.Fatalf("1+1 entries took %d statements, 12+40 took %d", few, many)
	}
	t.Logf("statements per ApplyLayout call: %d", many)
}
