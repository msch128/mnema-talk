//go:build integration

package media

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/msch128/mnema-talk/internal/db"
)

func TestPruneFailuresLeaveAccurateDatabaseState(t *testing.T) {
	f := mediaDB(t)
	id := f.attachment(t, "uploads/old", false)
	requireAPIError(t, func() error { _, err := PruneOlderThan(context.Background(), f.p, f.store, 0); return err }(), 400)
	f.store.batchErr = errors.New("storage refused delete")
	f.store.deleteErr = f.store.batchErr
	if n, err := PruneOlderThan(context.Background(), f.p, f.store, 1); n != 0 || err == nil || !f.store.Has("uploads/old") {
		t.Fatalf("failed prune n=%d %v", n, err)
	}
	f.store.batchErr, f.store.deleteErr = nil, nil
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.store.afterBatch = cancel
	if n, err := PruneOlderThan(ctx, f.p, f.store, 1); n != 0 || err == nil || !strings.Contains(err.Error(), "mark pruned media") {
		t.Fatalf("failed mark n=%d %v", n, err)
	}
	var deleted bool
	if err := f.p.QueryRow(context.Background(), `SELECT is_deleted FROM media WHERE id=$1`, id).Scan(&deleted); err != nil || deleted {
		t.Fatalf("mark persisted after canceled query=%v %v", deleted, err)
	}
}

func TestScheduledMediaTasksUseRealDatabaseAndRemainDefaultOff(t *testing.T) {
	var log bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&log, nil)))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })
	f := mediaDB(t)
	f.attachment(t, "uploads/old", false)
	retentionTask(f.p, f.store, 1)(context.Background())
	if f.store.Has("uploads/old") {
		t.Fatal("scheduled prune left old attachment")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	retentionTask(f.p, f.store, 1)(ctx)
	orphanScanTask(f.p, f.store)(context.Background())
	f.store.Put("avatars/orphan", []byte("old"), time.Now().Add(-72*time.Hour))
	orphanScanTask(f.p, f.store)(context.Background())
	if !f.store.Has("avatars/orphan") {
		t.Fatal("scheduled orphan scan deleted data")
	}
	f.store.listErr = errors.New("scan unavailable")
	orphanScanTask(f.p, f.store)(context.Background())
	StartRetentionWorker(ctx, f.p, f.store, 1)
	StartOrphanScan(ctx, f.p, f.store)
	StartOrphanScan(ctx, f.p, nil)
	for _, expected := range []string{"media pruned", "media pruning failed", "orphaned media objects found", "orphaned media scan failed"} {
		if !strings.Contains(log.String(), expected) {
			t.Fatalf("scheduled task did not report %q: %s", expected, log.String())
		}
	}
	if retentionFirstRun != time.Minute || orphanScanFirstRun != 10*time.Minute || retentionInterval != 24*time.Hour {
		t.Fatal("worker production schedule changed")
	}
}

func TestPrunePagesPastMaximumBatch(t *testing.T) {
	f := mediaDB(t)
	message := uuid.New()
	execMedia(t, f.p, `INSERT INTO messages(id,channel_id,user_id,content) VALUES($1,$2,$3,'old files')`, message, f.channel, f.user.ID)
	execMedia(t, f.p, `INSERT INTO media(uploader_id,channel_id,message_id,s3_bucket,s3_key,original_filename,mime_type,size_bytes,created_at)
 SELECT $1,$2,$3,'test-media','uploads/batch-' || n::text,'a.txt','text/plain',3,NOW()-INTERVAL '3 days' FROM generate_series(1,$4) n`, f.user.ID, f.channel, message, pruneBatch+1)
	for n := 1; n <= pruneBatch+1; n++ {
		f.store.Put("uploads/batch-"+strconv.Itoa(n), []byte("old"), time.Now().Add(-72*time.Hour))
	}
	n, err := PruneOlderThan(context.Background(), f.p, f.store, 1)
	if err != nil || n != pruneBatch+1 || f.store.Len() != 0 {
		t.Fatalf("batched prune=%d error=%v objects=%d", n, err, f.store.Len())
	}
	if n, err := PruneOlderThan(context.Background(), f.p, f.store, 1); err != nil || n != 0 {
		t.Fatalf("empty prune=%d %v", n, err)
	}
	stats, err := GetStats(context.Background(), f.p)
	if err != nil || stats.DeletedFiles != pruneBatch+1 || stats.TotalFiles != 0 {
		t.Fatalf("pruned stats=%+v %v", stats, err)
	}
}

// stuckKeyStore refuses to delete one key: a batch containing it fails as a
// whole, like an S3 DeleteObjects call with a per-key error.
type stuckKeyStore struct {
	*MemoryStore
	stuck string
}

func (s stuckKeyStore) Delete(ctx context.Context, key string) error {
	if key == s.stuck {
		return errors.New("object locked")
	}
	return s.MemoryStore.Delete(ctx, key)
}

func (s stuckKeyStore) DeleteBatch(ctx context.Context, keys []string) error {
	if slices.Contains(keys, s.stuck) {
		return errors.New("delete objects: 1 of 3 failed")
	}
	return s.MemoryStore.DeleteBatch(ctx, keys)
}

func TestPruneSkipsAKeyStorageRefusesToDelete(t *testing.T) {
	f := mediaDB(t)
	stuck := f.attachment(t, "uploads/stuck", false)
	f.attachment(t, "uploads/ok-1", false)
	f.attachment(t, "uploads/ok-2", false)
	store := stuckKeyStore{MemoryStore: f.store.MemoryStore, stuck: "uploads/stuck"}
	n, err := PruneOlderThan(context.Background(), f.p, store, 1)
	var skipped *PruneSkippedError
	if n != 2 || !errors.As(err, &skipped) || skipped.Skipped != 1 {
		t.Fatalf("prune n=%d err=%v, want the two healthy objects removed and an error for the stuck one", n, err)
	}
	if store.Has("uploads/ok-1") || store.Has("uploads/ok-2") || !store.Has("uploads/stuck") {
		t.Fatal("the stuck object blocked the rows behind it")
	}
	var deleted bool
	if err := f.p.QueryRow(context.Background(), `SELECT is_deleted FROM media WHERE id=$1`, stuck).Scan(&deleted); err != nil || deleted {
		t.Fatalf("stuck row is_deleted=%v err=%v", deleted, err)
	}
}

func TestManualPruneReportsTheCountWhenKeysAreSkipped(t *testing.T) {
	f := mediaDB(t)
	f.attachment(t, "uploads/stuck", false)
	f.attachment(t, "uploads/ok-1", false)
	f.attachment(t, "uploads/ok-2", false)
	f.h.Store = stuckKeyStore{MemoryStore: f.store.MemoryStore, stuck: "uploads/stuck"}
	rec := httptest.NewRecorder()
	if err := f.h.prune(rec, f.req(httptest.NewRequest(http.MethodPost, "/api/admin/media/prune?days=1", nil), "", "")); err != nil {
		t.Fatalf("prune with a stuck key: %v", err)
	}
	var res PruneResult
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK || res.PrunedCount != 2 || res.Skipped != 1 || res.CutoffDays != 1 {
		t.Fatalf("prune: %d %+v", rec.Code, res)
	}
}

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
