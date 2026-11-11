//go:build integration

package media

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/msch128/mnema-talk/internal/db"
)

func TestFindOrphansBatchesAndReportsStorageFailures(t *testing.T) {
	f := mediaDB(t)
	f.attachment(t, "uploads/known", false)
	for i := 0; i < orphanBatch+1; i++ {
		f.store.Put("uploads/"+uuid.NewString(), []byte("abc"), time.Now().Add(-72*time.Hour))
	}
	f.store.Put("uploads/fresh", []byte("fresh"), time.Now())
	f.store.Put("avatars/orphan", []byte("avatar"), time.Now().Add(-72*time.Hour))
	f.store.Put("unrelated/old", []byte("other"), time.Now().Add(-72*time.Hour))
	result, err := FindOrphans(context.Background(), f.p, f.store, true)
	if err != nil || result.Count != orphanBatch+2 || result.Deleted != orphanBatch+2 || result.Bytes != 3*(orphanBatch+1)+6 || !f.store.Has("uploads/fresh") || !f.store.Has("uploads/known") || !f.store.Has("unrelated/old") {
		t.Fatalf("orphans=%+v %v", result, err)
	}
	f.store.listErr = errors.New("list unavailable")
	if _, err := FindOrphans(context.Background(), f.p, f.store, false); !errors.Is(err, f.store.listErr) {
		t.Fatalf("list failure=%v", err)
	}
	if err := f.h.orphans(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil)); err == nil {
		t.Fatal("orphan handler lost scan failure")
	}
	f.store.listErr = nil
	f.store.Put("uploads/orphan", []byte("abc"), time.Now().Add(-72*time.Hour))
	f.store.batchErr = errors.New("delete unavailable")
	if o, err := FindOrphans(context.Background(), f.p, f.store, true); err == nil || !strings.Contains(err.Error(), "delete orphaned objects") || o.Deleted != 0 || !f.store.Has("uploads/orphan") {
		t.Fatalf("delete failure=%+v %v", o, err)
	}
	if err := f.h.cleanupOrphans(httptest.NewRecorder(), f.req(httptest.NewRequest(http.MethodPost, "/", nil), "", "")); err == nil {
		t.Fatal("cleanup lost delete failure")
	}
	f.store.batchErr = nil
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := FindOrphans(ctx, f.p, f.store, false); err == nil || !strings.Contains(err.Error(), "look up media keys") {
		t.Fatalf("lookup failure=%v", err)
	}
}

func TestOrphanScanReportsDatabaseFailureWhileReadingRows(t *testing.T) {
	f := mediaDB(t)
	f.attachment(t, "uploads/known", false)
	// Cache the SELECT on one real connection before taking a conflicting table lock.
	// The subsequent execution fails after Query returns its streaming rows.
	cfg := f.p.Config().Copy()
	cfg.MinConns = 0
	cfg.MaxConns = 1
	cfg.ConnConfig.RuntimeParams["lock_timeout"] = "20ms"
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	p := &db.Pool{Pool: pool}
	if _, err := FindOrphans(context.Background(), p, f.store, false); err != nil {
		t.Fatal(err)
	}
	tx, err := f.p.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(context.Background(), `LOCK TABLE media IN ACCESS EXCLUSIVE MODE`); err != nil {
		t.Fatal(err)
	}
	result, err := FindOrphans(context.Background(), p, f.store, true)
	var pgerr *pgconn.PgError
	if !errors.As(err, &pgerr) || pgerr.Code != "55P03" || strings.Contains(err.Error(), "look up media keys") || result.Deleted != 0 || !f.store.Has("uploads/known") {
		t.Fatalf("streamed query failure result=%+v error=%v", result, err)
	}
}
