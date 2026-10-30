//go:build integration

package media

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/httpx"
)

func (f *mediaFixture) staged(t *testing.T, key string, age time.Duration) uuid.UUID {
	t.Helper()
	id := uuid.New()
	execMedia(t, f.p, `INSERT INTO media(id,uploader_id,channel_id,s3_bucket,s3_key,original_filename,mime_type,size_bytes,staged,created_at)
		VALUES($1,$2,$3,'test-media',$4,'a.txt','text/plain',3,TRUE,NOW()-make_interval(secs => $5))`, id, f.user.ID, f.channel, key, age.Seconds())
	f.store.Put(key, []byte("abc"), time.Now().Add(-age))
	return id
}

func (f *mediaFixture) stagedRows(t *testing.T) int {
	t.Helper()
	var n int
	if err := f.p.QueryRow(context.Background(), `SELECT count(*) FROM media WHERE staged`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestTheStagedSweepTaskRemovesOnlyExpiredFiles(t *testing.T) {
	f := mediaDB(t)
	f.staged(t, "uploads/old", 7*time.Hour)
	f.staged(t, "uploads/new", time.Minute)
	stagedSweepTask(f.p, f.store)(context.Background())
	if f.stagedRows(t) != 1 || f.store.Has("uploads/old") || !f.store.Has("uploads/new") {
		t.Fatalf("rows=%d old=%v new=%v", f.stagedRows(t), f.store.Has("uploads/old"), f.store.Has("uploads/new"))
	}
	// Nothing left to remove, and a database that fails, are only logged.
	stagedSweepTask(f.p, f.store)(context.Background())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	stagedSweepTask(f.p, f.store)(ctx)
}

func TestTheSweepReportsAStorageFailureButKeepsNoRow(t *testing.T) {
	f := mediaDB(t)
	f.staged(t, "uploads/stuck", 7*time.Hour)
	store := stuckKeyStore{MemoryStore: f.store.MemoryStore, stuck: "uploads/stuck"}
	n, err := SweepStaged(context.Background(), f.p, store, 6*time.Hour)
	if err == nil || n != 1 {
		t.Fatalf("swept %d, err %v; want the row gone and the failure reported", n, err)
	}
	if f.stagedRows(t) != 0 {
		t.Fatal("the row must be gone even though the object stayed (the orphan scan reports it)")
	}
}

func TestSweepFailsOnADatabaseError(t *testing.T) {
	f := mediaDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := SweepStaged(ctx, f.p, f.store, time.Hour); err == nil {
		t.Fatal("a failing database must be reported")
	}
}

func TestTheCapOfUnsentFilesIsEnforcedAndTheObjectDiscarded(t *testing.T) {
	f := mediaDB(t)
	for i := 0; i < maxStagedPerMember; i++ {
		f.staged(t, "uploads/held-"+uuid.NewString(), time.Minute)
	}
	f.store.Put("uploads/extra", []byte("abc"), time.Now())
	err := f.h.insertStaged(context.Background(), f.channel, f.user.ID, &storedUpload{id: uuid.New(), key: "uploads/extra", filename: "x.txt", mime: "text/plain", size: 3})
	if _, refused := httpx.AsAPIError(err); !refused {
		t.Fatalf("want a refusal, got %v", err)
	}
	if f.store.Has("uploads/extra") {
		t.Fatal("the refused file must not stay in storage")
	}
}

func TestStagingReportsADatabaseFailureAndDiscardsTheObject(t *testing.T) {
	f := mediaDB(t)
	f.store.Put("uploads/lost", []byte("abc"), time.Now())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := f.h.insertStaged(ctx, f.channel, f.user.ID, &storedUpload{id: uuid.New(), key: "uploads/lost", filename: "x.txt", mime: "text/plain", size: 3})
	if err == nil {
		t.Fatal("expected a failure")
	}
	if _, refused := httpx.AsAPIError(err); refused {
		t.Fatalf("a database failure must not look like a refusal: %v", err)
	}
	if f.store.Has("uploads/lost") {
		t.Fatal("the object must not outlive a failed write")
	}
}

func (f *mediaFixture) unstage(ctx context.Context, channel, id string) (int, error) {
	rc := chi.NewRouteContext()
	rc.URLParams.Add("channelID", channel)
	rc.URLParams.Add("id", id)
	r := httptest.NewRequest(http.MethodDelete, "/", nil).WithContext(context.WithValue(auth.WithUser(ctx, f.user), chi.RouteCtxKey, rc))
	w := httptest.NewRecorder()
	err := f.h.unstageAttachment(w, r)
	return w.Code, err
}

func TestUnstageRemovesOnlyTheOwnersUnsentFile(t *testing.T) {
	f := mediaDB(t)
	mine := f.staged(t, "uploads/mine", time.Minute)
	ctx := context.Background()
	if _, err := f.unstage(ctx, "nope", mine.String()); err == nil {
		t.Fatal("a bad channel id is refused")
	}
	if _, err := f.unstage(ctx, f.channel.String(), "nope"); err == nil {
		t.Fatal("a bad attachment id is refused")
	}
	if _, err := f.unstage(ctx, f.channel.String(), uuid.NewString()); err == nil {
		t.Fatal("an unknown file is not found")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := f.unstage(cancelled, f.channel.String(), mine.String()); err == nil {
		t.Fatal("a database failure is reported")
	}
	if code, err := f.unstage(ctx, f.channel.String(), mine.String()); err != nil || code != http.StatusNoContent {
		t.Fatalf("remove: %d %v", code, err)
	}
	if f.store.Has("uploads/mine") || f.stagedRows(t) != 0 {
		t.Fatal("the file and its row are gone")
	}
}

func TestTheSweepWorkerStartsOnlyWithStorageAndStopsWithItsContext(t *testing.T) {
	f := mediaDB(t)
	StartStagedSweep(context.Background(), f.p, nil)
	ctx, cancel := context.WithCancel(context.Background())
	StartStagedSweep(ctx, f.p, f.store)
	cancel()
}
