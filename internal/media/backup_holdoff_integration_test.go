//go:build integration

package media

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// backupRunning holds the lock an online backup takes until the test ends.
func (f *mediaFixture) backupRunning(t *testing.T) {
	t.Helper()
	conn, err := f.p.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(context.Background(), `SELECT pg_advisory_lock($1)`, db.BackupLockID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = conn.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, db.BackupLockID)
		conn.Release()
	})
}

// untilStopped is a context that ends shortly: a task that waits for the
// backup returns then, like at a server shutdown.
func untilStopped(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	t.Cleanup(cancel)
	return ctx
}

func TestTheStagedSweepHoldsOffDuringABackup(t *testing.T) {
	f := mediaDB(t)
	f.staged(t, "uploads/old", 7*time.Hour)
	f.backupRunning(t)
	stagedSweepTask(f.p, f.store)(untilStopped(t))
	if f.stagedRows(t) != 1 || !f.store.Has("uploads/old") {
		t.Fatalf("rows=%d object=%v; the sweep ran during a backup", f.stagedRows(t), f.store.Has("uploads/old"))
	}
}

func TestMediaPruningHoldsOffDuringABackup(t *testing.T) {
	f := mediaDB(t)
	id := f.attachment(t, "uploads/expired", false)
	execMedia(t, f.p, `UPDATE media SET created_at = NOW() - INTERVAL '30 days' WHERE id = $1`, id)
	f.backupRunning(t)
	retentionTask(f.p, f.store, 7)(untilStopped(t))
	if !f.store.Has("uploads/expired") {
		t.Fatal("pruning deleted an object during a backup")
	}
	var deleted bool
	if err := f.p.QueryRow(context.Background(), `SELECT is_deleted FROM media WHERE id = $1`, id).Scan(&deleted); err != nil || deleted {
		t.Fatalf("is_deleted=%v err=%v; pruning ran during a backup", deleted, err)
	}
}

func TestOrphanCleanupIsRefusedDuringABackup(t *testing.T) {
	f := mediaDB(t)
	f.store.Put("uploads/orphan", []byte("x"), time.Now().Add(-48*time.Hour))
	f.backupRunning(t)
	err := f.h.cleanupOrphans(httptest.NewRecorder(), f.req(httptest.NewRequest(http.MethodPost, "/", nil), "", ""))
	var apiErr *httpx.APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusConflict {
		t.Fatalf("err = %v, want 409", err)
	}
	if !f.store.Has("uploads/orphan") {
		t.Fatal("the cleanup deleted an object during a backup")
	}
	// The daily report waits as well: the storage is frozen while it is copied.
	listed := &listCountingStore{Store: f.store}
	orphanScanTask(f.p, listed)(untilStopped(t))
	if listed.calls != 0 {
		t.Fatal("the orphan report listed the storage during a backup")
	}
}

type listCountingStore struct {
	Store
	calls int
}

func (s *listCountingStore) List(ctx context.Context, prefix string, fn func(ObjectInfo) error) error {
	s.calls++
	return s.Store.List(ctx, prefix, fn)
}
