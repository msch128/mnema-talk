//go:build integration

package media

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/msch128/mnema-talk/internal/db"
)

// A schema that returns rows the code cannot scan must fail the prune before
// anything is deleted from storage or marked in the database. The shadow
// schema only exists on this test's own connection (search_path), so the real
// media table is never touched.
func TestPruneRejectsUnscannableRows(t *testing.T) {
	f := mediaDB(t)
	f.attachment(t, "uploads/old", false)
	ctx := context.Background()
	if _, err := f.p.Exec(ctx, `CREATE SCHEMA media_scan_fixture;
		CREATE VIEW media_scan_fixture.media AS SELECT
		'invalid-uuid'::text AS id, 'uploads/old'::text AS s3_key, NOW() - INTERVAL '3 days' AS created_at,
		false AS is_deleted, 'a-message'::text AS message_id`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := f.p.Exec(ctx, `DROP SCHEMA media_scan_fixture CASCADE`); err != nil {
			t.Error(err)
		}
	})
	config := f.p.Config().Copy()
	config.ConnConfig.RuntimeParams["search_path"] = "media_scan_fixture,public"
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	n, err := PruneOlderThan(ctx, &db.Pool{Pool: pool}, f.store, 1)
	if n != 0 || err == nil || !strings.Contains(err.Error(), "invalid UUID") {
		t.Fatalf("unscannable prune n=%d err=%v", n, err)
	}
	if !f.store.Has("uploads/old") {
		t.Fatal("failed prune deleted the stored object")
	}
}
