//go:build integration

package wiki

import (
	"context"
	"testing"

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
