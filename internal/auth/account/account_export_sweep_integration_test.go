//go:build integration

package account

import (
	"context"
	"errors"
	"testing"
)

// failingDelete is a store whose removals fail, as object storage does when it
// is down.
type failingDelete struct{ *memStore }

func (failingDelete) Delete(context.Context, string) error { return errors.New("storage down") }

// A sweep that cannot remove the files of spent exports has failed: the job
// metrics must say so, and the row keeps its key so the next run tries again.
func TestExportSweepReportsFilesItCouldNotRemove(t *testing.T) {
	h, pool, member, store, _ := exportFixture(t)
	id, err := runExport(t, h, member.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), `UPDATE data_exports SET expires_at = NOW() - INTERVAL '1 hour' WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	h.Objects = failingDelete{store}
	if err := h.sweepExports(context.Background()); err == nil {
		t.Fatal("a sweep that could not remove a file reported success")
	}
	var kept bool
	if err := pool.QueryRow(context.Background(), `SELECT s3_key IS NOT NULL FROM data_exports WHERE id = $1`, id).Scan(&kept); err != nil || !kept {
		t.Fatalf("the key must stay for the next run: kept=%v err=%v", kept, err)
	}

	h.Objects = store
	if err := h.sweepExports(context.Background()); err != nil {
		t.Fatalf("a healthy sweep failed: %v", err)
	}
	if store.count() != 0 {
		t.Fatal("the expired file was not removed")
	}
}
