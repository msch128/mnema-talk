//go:build integration

package chat

import (
	"context"
	"testing"

	"github.com/msch128/mnema-talk/internal/chat/channels"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/testutil/pgfault"
)

// Every statement of a category deletion can fail; each failure is reported
// and leaves the category in place.
func TestDeleteCategoryReportsEveryDatabaseFailure(t *testing.T) {
	f := newChatFixture(t)
	ctx := context.Background()
	cat, err := channels.CreateCategory(ctx, f.p, "kept", 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		"SELECT 1 FROM categories WHERE id",
		"SELECT EXISTS (SELECT 1 FROM permission_overwrites",
		"INSERT INTO permission_overwrites",
		"DELETE FROM categories",
	} {
		pool := &db.Pool{Pool: pgfault.Pool(t, f.p.Pool, &pgfault.Tracer{Query: query})}
		if err := channels.DeleteCategory(ctx, pool, cat.ID); err == nil {
			t.Fatalf("%q failing: the deletion reported success", query)
		}
	}
	var n int
	if err := f.p.QueryRow(ctx, `SELECT COUNT(*) FROM categories WHERE id = $1`, cat.ID).Scan(&n); err != nil || n != 1 {
		t.Fatalf("category rows = %d, %v, want it kept", n, err)
	}
}
