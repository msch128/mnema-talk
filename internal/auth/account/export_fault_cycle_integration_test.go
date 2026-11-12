//go:build integration

package account

import (
	"context"
	"errors"
	"io"
	"os"
	"runtime"
	"testing"
	"time"
)

// Lane 8 cycle test for exports: builds started through the real background
// path whose object-store upload fails, after k bytes (a partial write) for a
// spread of k, or at once. After every round the export is failed (so it does
// not count toward the daily limit), no object is left, the build slot is free
// and the archive writer is gone; after all rounds the goroutine count is at
// the baseline and a working store gets a ready export. EXPORT_CYCLE_DEEP=1
// cuts at more offsets.

// cutStore takes the first n bytes of an upload and then fails, as an object
// store that times out in the middle does; n < 0 refuses before reading.
type cutStore struct {
	*memStore
	n int
}

var errStoreCut = errors.New("object store: connection reset")

func (s *cutStore) Upload(_ context.Context, _ string, body io.Reader, _ string, _ int64) error {
	if s.n < 0 {
		return errStoreCut
	}
	if _, err := io.CopyN(io.Discard, body, int64(s.n)); err != nil {
		return err
	}
	return errStoreCut
}

func TestExportsWhoseUploadFailsLeaveNothingBehind(t *testing.T) {
	h, pool, member, store, channel := exportFixture(t)
	seedMessages(t, pool, member.ID, channel, 300)
	ctx := context.Background()
	cuts := []int{-1, 0, 1, 22, 512, 4096, 32 << 10}
	if os.Getenv("EXPORT_CYCLE_DEEP") == "1" {
		for k := 2; k < 64<<10; k = k*3/2 + 1 {
			cuts = append(cuts, k)
		}
	}
	goBase := runtime.NumGoroutine()
	failed := 0
	for _, cut := range cuts {
		h.Objects = &cutStore{memStore: store, n: cut}
		id, wait, err := h.claimExport(ctx, member.ID)
		if err != nil || wait > 0 {
			t.Fatalf("cut %d: claim refused (wait %v, %v): a failed export counted toward the daily limit", cut, wait, err)
		}
		if h.buildExportInBackground(id, member.ID) != nil {
			t.Fatalf("cut %d: no build slot free: an earlier build kept its slot", cut)
		}
		// Not Drain: from its start on it refuses every new job, and the next
		// cut needs another build.
		h.DrainMail()
		var status string
		if err := pool.QueryRow(ctx, `SELECT status FROM data_exports WHERE id = $1`, id).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if status != "failed" {
			t.Fatalf("cut %d: export is %q after the upload failed, want failed", cut, status)
		}
		if store.count() != 0 {
			t.Fatalf("cut %d: %d objects left", cut, store.count())
		}
		if len(h.exportSlots) != 0 {
			t.Fatalf("cut %d: %d build slots still taken", cut, len(h.exportSlots))
		}
		failed++
	}
	if failed != len(cuts) {
		t.Fatalf("only %d of %d failing builds ran", failed, len(cuts))
	}
	gor := runtime.NumGoroutine()
	for deadline := time.Now().Add(5 * time.Second); gor > goBase+2 && time.Now().Before(deadline); gor = runtime.NumGoroutine() {
		time.Sleep(20 * time.Millisecond)
	}
	if gor > goBase+2 {
		t.Errorf("goroutines %d after %d failed builds, baseline %d", gor, failed, goBase)
	}
	// The same account then gets a working export.
	h.Objects = store
	id, wait, err := h.claimExport(ctx, member.ID)
	if err != nil || wait > 0 {
		t.Fatalf("claim after the failures: wait %v, %v", wait, err)
	}
	if h.buildExportInBackground(id, member.ID) != nil || !h.Drain(30*time.Second) {
		t.Fatal("the working build did not run")
	}
	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM data_exports WHERE id = $1`, id).Scan(&status); err != nil || status != "ready" {
		t.Fatalf("working export is %q (%v), want ready", status, err)
	}
	t.Logf("failed builds %d (cuts %v); objects, slots, goroutines at baseline; then one ready export", failed, cuts)
}
