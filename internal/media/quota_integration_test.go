//go:build integration

package media

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

func uploadOf(t *testing.T, f *mediaFixture, size int) error {
	t.Helper()
	body := make([]byte, size)
	copy(body, "plain text")
	for i := 10; i < size; i++ {
		body[i] = 'a'
	}
	r := f.req(uploadRequest(t, formPart{name: "file", filename: "a.txt", mime: "text/plain", value: string(body)}), "channelID", f.channel.String())
	r.ContentLength = int64(size)
	return f.h.upload(httptest.NewRecorder(), r)
}

func TestUploadQuotaPerUserRollingWindow(t *testing.T) {
	f := mediaDB(t)
	f.h.MaxUploadBytes = 1 << 20
	f.h.Quota = Quota{PerUserBytes: 1000}

	if err := uploadOf(t, f, 600); err != nil {
		t.Fatalf("first upload within budget: %v", err)
	}
	// 600 used, 600 more would pass 1000.
	requireAPIError(t, uploadOf(t, f, 600), http.StatusTooManyRequests)

	// Deleting the file does not give the budget back.
	execMedia(t, f.p, `UPDATE media SET is_deleted = TRUE WHERE uploader_id = $1`, f.user.ID)
	requireAPIError(t, uploadOf(t, f, 600), http.StatusTooManyRequests)

	// Uploads older than the window no longer count.
	execMedia(t, f.p, `UPDATE media SET created_at = NOW() - INTERVAL '25 hours' WHERE uploader_id = $1`, f.user.ID)
	if err := uploadOf(t, f, 600); err != nil {
		t.Fatalf("upload after the window passed: %v", err)
	}
}

func TestUploadQuotaIsPerAccountAndOffByDefault(t *testing.T) {
	f := mediaDB(t)
	f.h.MaxUploadBytes = 1 << 20
	other := uuid.New()
	execMedia(t, f.p, `INSERT INTO users(id,username,display_name,password_hash,role) VALUES($1,'other','Other','x','user')`, other)
	execMedia(t, f.p, `INSERT INTO media(id,uploader_id,s3_bucket,s3_key,original_filename,mime_type,size_bytes) VALUES($1,$2,'b','k','a','text/plain',900)`, uuid.New(), other)

	f.h.Quota = Quota{PerUserBytes: 1000}
	if err := f.h.CheckUploadQuota(context.Background(), f.user.ID, 900); err != nil {
		t.Fatalf("another account's usage must not count: %v", err)
	}
	requireAPIError(t, f.h.CheckUploadQuota(context.Background(), other, 200), http.StatusTooManyRequests)

	f.h.Quota = Quota{}
	if err := f.h.CheckUploadQuota(context.Background(), other, 1<<30); err != nil {
		t.Fatalf("no quota configured: %v", err)
	}
}

func TestUploadRefusedWhenStorageIsFull(t *testing.T) {
	f := mediaDB(t)
	f.h.MaxUploadBytes = 1 << 20
	execMedia(t, f.p, `INSERT INTO media(id,uploader_id,s3_bucket,s3_key,original_filename,mime_type,size_bytes) VALUES($1,$2,'b','k1','a','text/plain',800)`, uuid.New(), f.user.ID)
	execMedia(t, f.p, `INSERT INTO media(id,uploader_id,s3_bucket,s3_key,original_filename,mime_type,size_bytes,is_deleted) VALUES($1,$2,'b','k2','a','text/plain',5000,TRUE)`, uuid.New(), f.user.ID)

	f.h.Quota = Quota{TotalBytes: 1000}
	// Deleted files do not count towards the cap.
	if err := uploadOf(t, f, 100); err != nil {
		t.Fatalf("upload under the cap: %v", err)
	}
	requireAPIError(t, uploadOf(t, f, 200), http.StatusServiceUnavailable)
}

func TestUploadRefusedBelowFreeSpaceFloor(t *testing.T) {
	f := mediaDB(t)
	f.h.Quota = Quota{MinFreeBytes: 1 << 60, DiskPath: t.TempDir()}
	requireAPIError(t, f.h.CheckUploadQuota(context.Background(), f.user.ID, 1), http.StatusServiceUnavailable)

	// An unreadable path must not block uploads (the guard is advisory).
	f.h.Quota = Quota{MinFreeBytes: 1 << 60, DiskPath: "/does/not/exist"}
	if err := f.h.CheckUploadQuota(context.Background(), f.user.ID, 1); err != nil {
		t.Fatalf("unreadable path: %v", err)
	}
}
