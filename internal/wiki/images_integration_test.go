//go:build integration

package wiki

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/imgproc"
	"github.com/msch128/mnema-talk/internal/media"
)

// addPictures marks n new media rows as wiki pictures made at the given age;
// the first of them (the oldest) is returned.
func (w *world) addPictures(t *testing.T, n int, age string) string {
	t.Helper()
	ctx := context.Background()
	if _, err := w.p.Exec(ctx, `
		INSERT INTO media (id, uploader_id, s3_bucket, s3_key, original_filename, mime_type, size_bytes)
		SELECT gen_random_uuid(), $1, 'b', 'wiki/seed-' || gen_random_uuid(), 'wiki.png', 'image/png', 1
		FROM generate_series(1, $2)`, w.alice, n); err != nil {
		t.Fatal(err)
	}
	if _, err := w.p.Exec(ctx, `
		INSERT INTO wiki_images (media_id, created_at)
		SELECT m.id, NOW() - $1::interval - make_interval(secs => row_number() OVER (ORDER BY m.id))
		FROM media m WHERE NOT EXISTS (SELECT 1 FROM wiki_images i WHERE i.media_id = m.id)`, age); err != nil {
		t.Fatal(err)
	}
	var oldest string
	if err := w.p.QueryRow(ctx, `SELECT media_id::text FROM wiki_images ORDER BY created_at LIMIT 1`).Scan(&oldest); err != nil {
		t.Fatal(err)
	}
	return oldest
}

func (w *world) pictureCount(t *testing.T) int {
	t.Helper()
	var n int
	if err := w.p.QueryRow(context.Background(), `SELECT count(*) FROM wiki_images`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestBelowTheCapNothingIsRemovedAndAtTheCapOnlyUnusedOldPicturesGo(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	store := media.NewMemoryStore()

	w.addPictures(t, MaxImages-1, "2 days")
	if err := w.s.makeRoom(ctx, store); err != nil || w.pictureCount(t) != MaxImages-1 {
		t.Fatalf("below the cap: %v, %d pictures", err, w.pictureCount(t))
	}

	// One more fills the cap. The oldest picture is in use by a revision, so it stays when room is made.
	used := w.addPictures(t, 1, "3 days")
	if _, err := w.s.Create(ctx, w.alice, PageInput{Title: "With picture", Content: "see ![](/api/media/" + used + ")"}); err != nil {
		t.Fatal(err)
	}
	if err := w.s.makeRoom(ctx, store); err != nil {
		t.Fatalf("at the cap with unused pictures: %v", err)
	}
	if got := w.pictureCount(t); got != MaxImages-imageSweep {
		t.Fatalf("pictures after making room: %d, want %d", got, MaxImages-imageSweep)
	}
	var kept bool
	if err := w.p.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM wiki_images WHERE media_id = $1::uuid)`, used).Scan(&kept); err != nil || !kept {
		t.Fatalf("the picture a revision uses was removed (%v)", err)
	}
}

func TestAtTheCapWithoutRoomTheUploadIsRefused(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	// All of them are new: their pages may still be written.
	w.addPictures(t, MaxImages, "0 seconds")
	if err := w.s.makeRoom(ctx, media.NewMemoryStore()); !is(err, errTooManyImages) {
		t.Fatalf("at the cap, all new: %v", err)
	}
	if w.pictureCount(t) != MaxImages {
		t.Fatalf("a refused upload removed pictures: %d", w.pictureCount(t))
	}
}

// brokenStore refuses to write or delete, like object storage that is down.
type brokenStore struct{ *media.MemoryStore }

func (brokenStore) Upload(context.Context, string, io.Reader, string, int64) error {
	return errors.New("storage is down")
}

func (brokenStore) Delete(context.Context, string) error { return errors.New("storage is down") }

func TestAPictureThatCannotBeStoredLeavesNothingBehind(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	pic := &imgproc.Picture{Data: []byte("png"), ContentType: "image/png"}

	h := &Handler{Store: w.s, MediaStore: brokenStore{media.NewMemoryStore()}, MediaBucket: "b"}
	if _, err := h.storeImage(ctx, w.alice, pic); err == nil {
		t.Fatal("an upload to storage that is down was accepted")
	}

	// The object went up but the rows could not be written (no such uploader): the object is removed again.
	mem := media.NewMemoryStore()
	h = &Handler{Store: w.s, MediaStore: mem, MediaBucket: "b"}
	if _, err := h.storeImage(ctx, uuid.New(), pic); err == nil {
		t.Fatal("a picture of an unknown uploader was accepted")
	}
	if mem.Len() != 0 || w.pictureCount(t) != 0 {
		t.Fatalf("left behind: %d objects, %d rows", mem.Len(), w.pictureCount(t))
	}
}

func TestMakingRoomSkipsAPictureThatCannotBeDeletedFromStorage(t *testing.T) {
	w := newWorld(t)
	w.addPictures(t, MaxImages, "2 days")
	if err := w.s.makeRoom(context.Background(), brokenStore{media.NewMemoryStore()}); err != nil {
		t.Fatalf("make room with storage down: %v", err)
	}
	if w.pictureCount(t) != MaxImages {
		t.Fatalf("pictures whose object could not be deleted were forgotten: %d", w.pictureCount(t))
	}
}

func TestOneMemberCannotFillTheRoomOfTheWiki(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	pic := &imgproc.Picture{Data: []byte("png"), ContentType: "image/png"}
	h := &Handler{Store: w.s, MediaStore: media.NewMemoryStore(), MediaBucket: "b"}
	for i := 0; i < MaxImagesPerDay; i++ {
		if _, err := h.storeImage(ctx, w.alice, pic); err != nil {
			t.Fatalf("picture %d: %v", i, err)
		}
	}
	if _, err := h.storeImage(ctx, w.alice, pic); !is(err, errImagesToday) {
		t.Fatalf("the picture after the daily number: %v", err)
	}
	if _, err := h.storeImage(ctx, w.bob, pic); err != nil {
		t.Fatalf("somebody else is not held back by it: %v", err)
	}
}

func TestParallelUploadsCannotPassTheCapTogether(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	w.addPictures(t, MaxImages-1, "2 days")
	pic := &imgproc.Picture{Data: []byte("png"), ContentType: "image/png"}
	mem := media.NewMemoryStore()
	h := &Handler{Store: w.s, MediaStore: mem, MediaBucket: "b"}
	var wg sync.WaitGroup
	results := make([]error, 8)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, results[i] = h.storeImage(ctx, w.bob, pic)
		}()
	}
	wg.Wait()
	ok := 0
	for _, err := range results {
		if err == nil {
			ok++
		} else if !is(err, errTooManyImages) {
			t.Fatalf("unexpected: %v", err)
		}
	}
	if ok != 1 || w.pictureCount(t) != MaxImages || mem.Len() != 1 {
		t.Fatalf("%d went through, %d pictures, %d objects", ok, w.pictureCount(t), mem.Len())
	}
}

func TestAWikiPictureCountsAgainstTheMembersDailyUploadBudget(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	pic := &imgproc.Picture{Data: make([]byte, 600<<10), ContentType: "image/png"}
	h := &Handler{Store: w.s, MediaStore: media.NewMemoryStore(), MediaBucket: "b"}
	if _, err := h.storeImage(ctx, w.alice, pic); err != nil {
		t.Fatal(err)
	}
	uploads := &media.Handler{DB: w.p, Quota: media.Quota{PerUserBytes: 1 << 20}}
	if _, err := uploads.ReserveUpload(ctx, w.alice, 600<<10); err == nil {
		t.Fatal("a second 600 KB upload passed a 1 MB daily budget after a wiki picture of 600 KB")
	}
	if release, err := uploads.ReserveUpload(ctx, w.bob, 600<<10); err != nil {
		t.Fatalf("somebody else is not held back by it: %v", err)
	} else {
		release()
	}
}
