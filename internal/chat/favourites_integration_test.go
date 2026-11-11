//go:build integration

package chat

import (
	"context"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat/dm"
)

func TestFavouritesReportOnlyWhatChanged(t *testing.T) {
	f := newChatFixture(t)
	ctx := context.Background()
	var dms []uuid.UUID
	for _, name := range []string{"fav_a", "fav_b", "fav_c"} {
		id, _, err := dm.CreateDM(ctx, f.p, dm.NewDM{Creator: f.u.ID, Others: []uuid.UUID{newMember(t, f, name)}})
		if err != nil {
			t.Fatal(err)
		}
		dms = append(dms, id)
	}
	a, b, c := dms[0], dms[1], dms[2]
	changed := func(ids ...uuid.UUID) []uuid.UUID {
		t.Helper()
		got, err := dm.SetDMFavourites(ctx, f.p, f.u.ID, ids)
		if err != nil {
			t.Fatal(err)
		}
		slices.SortFunc(got, func(x, y uuid.UUID) int { return slices.Compare(x[:], y[:]) })
		return got
	}
	sorted := func(ids ...uuid.UUID) []uuid.UUID {
		slices.SortFunc(ids, func(x, y uuid.UUID) int { return slices.Compare(x[:], y[:]) })
		return ids
	}
	if got := changed(a, b); !slices.Equal(got, sorted(a, b)) {
		t.Fatalf("first favourites: %v", got)
	}
	if got := changed(a, b); len(got) != 0 {
		t.Fatalf("same favourites again: %v", got)
	}
	if got := changed(a, b, c); !slices.Equal(got, []uuid.UUID{c}) {
		t.Fatalf("added c: %v", got)
	}
	if got := changed(b, a, c); !slices.Equal(got, sorted(a, b)) {
		t.Fatalf("swapped a and b: %v", got)
	}
	if got := changed(b, a); !slices.Equal(got, []uuid.UUID{c}) {
		t.Fatalf("dropped c: %v", got)
	}
}
