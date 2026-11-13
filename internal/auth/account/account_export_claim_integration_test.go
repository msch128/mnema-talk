//go:build integration

package account

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/testutil/racetest"
)

func TestOnlyOneOfParallelDownloadsHoldsTheExport(t *testing.T) {
	h, pool, member, _, _ := exportFixture(t)
	ctx := context.Background()
	id := readyExport(t, h, member)
	racetest.Sweep(t, racetest.Rounds(10, 300), 6,
		func(int) {
			if _, err := pool.Exec(ctx, `UPDATE data_exports SET claimed_at = NULL WHERE id = $1`, id); err != nil {
				t.Fatal(err)
			}
		},
		func(_, _ int) error {
			_, err := h.claimDownload(ctx, member.ID)
			return err
		},
		func(round int, r racetest.Round) string {
			if r.Succeeded() != 1 {
				return fmt.Sprintf("round %d: %d downloads held the export at once", round, r.Succeeded())
			}
			return ""
		}).FailOnViolation(t)
}

func TestABodyShorterThanItsLengthDoesNotSpendTheExport(t *testing.T) {
	h, pool, member, _, _ := exportFixture(t)
	id := readyExport(t, h, member)
	// The store hands over fewer bytes than the export says it has.
	if _, err := pool.Exec(context.Background(), `UPDATE data_exports SET size_bytes = size_bytes + 100 WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	download(h, member, httptest.NewRecorder(), "same-origin")
	if row := readExportRow(t, pool, id); row.downloaded || row.claimed || !row.hasKey {
		t.Fatalf("a short copy must leave the export as it was: %+v", row)
	}
}

func TestTheSweepLeavesAFileWhileADownloadHoldsIt(t *testing.T) {
	h, pool, member, store, _ := exportFixture(t)
	ctx := context.Background()
	id := readyExport(t, h, member)
	download(h, member, httptest.NewRecorder(), "same-origin")
	// A second download started in the last minutes of the hour is still running.
	if _, err := pool.Exec(ctx, `UPDATE data_exports SET downloaded_at = NOW() - INTERVAL '61 minutes', claimed_at = NOW() - INTERVAL '2 minutes' WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	if err := h.sweepExports(ctx); err != nil {
		t.Fatal(err)
	}
	if row := readExportRow(t, pool, id); !row.hasKey || store.count() != 1 {
		t.Fatalf("the sweep removed the file under a running download: %+v files=%d", row, store.count())
	}
	// A claim older than ten minutes belongs to no running download.
	if _, err := pool.Exec(ctx, `UPDATE data_exports SET claimed_at = NOW() - INTERVAL '11 minutes' WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	if err := h.sweepExports(ctx); err != nil {
		t.Fatal(err)
	}
	if row := readExportRow(t, pool, id); row.hasKey || store.count() != 0 {
		t.Fatalf("after the claim the sweep removes the file: %+v files=%d", row, store.count())
	}
}

func TestAnOldDownloadFinishingLeavesANewerOnesClaim(t *testing.T) {
	h, pool, member, _, _ := exportFixture(t)
	ctx := context.Background()
	id := readyExport(t, h, member)
	first, err := h.claimDownload(ctx, member.ID)
	if err != nil {
		t.Fatal(err)
	}
	// The first download outlived its ten minutes and a second one claimed the export.
	if _, err := pool.Exec(ctx, `UPDATE data_exports SET claimed_at = NOW() - INTERVAL '11 minutes' WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	stale := first
	if err := pool.QueryRow(ctx, `SELECT claimed_at FROM data_exports WHERE id = $1`, id).Scan(&stale.claimedAt); err != nil {
		t.Fatal(err)
	}
	if _, err := h.claimDownload(ctx, member.ID); err != nil {
		t.Fatal(err)
	}
	h.finishDownload(ctx, member, stale)
	if row := readExportRow(t, pool, id); !row.downloaded || !row.claimed {
		t.Fatalf("the finished copy counts and the newer claim stays: %+v", row)
	}
	h.releaseDownload(ctx, stale)
	if row := readExportRow(t, pool, id); !row.claimed {
		t.Fatalf("releasing the old claim freed the newer one: %+v", row)
	}
}

func TestACompleteDownloadIsAuditedWithItsRecord(t *testing.T) {
	h, pool, member, _, _ := exportFixture(t)
	h.Audit = &audit.Log{DB: pool}
	ctx := context.Background()
	readyExport(t, h, member)
	download(h, member, httptest.NewRecorder(), "same-origin")
	download(h, member, failingWriter{httptest.NewRecorder()}, "same-origin")
	var audited int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM audit_log WHERE action = 'auth.export_download'`).Scan(&audited); err != nil || audited != 1 {
		t.Fatalf("one complete download, one cut: %d audit entries (%v)", audited, err)
	}
}

func TestTheArchiveSaysHowLongItCanBeDownloaded(t *testing.T) {
	h, _, member, _, _ := exportFixture(t)
	readyExport(t, h, member)
	w := httptest.NewRecorder()
	download(h, member, w, "same-origin")
	zr, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	f, err := zr.Open("README.txt")
	if err != nil {
		t.Fatal(err)
	}
	text, _ := io.ReadAll(f)
	flat := strings.Join(strings.Fields(string(text)), " ")
	if strings.Contains(flat, "available for download once") || !strings.Contains(flat, "removed one hour after the first complete download") {
		t.Fatalf("README.txt: %s", text)
	}
	if w.Code != http.StatusOK {
		t.Fatal(w.Code)
	}
}
