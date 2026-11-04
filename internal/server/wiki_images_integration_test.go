//go:build integration

package server

import (
	"bytes"
	"context"
	"image"
	"net/http"
	"strings"
	"testing"

	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/wiki"
)

func (f *wikiFixture) uploadImage(c *client, filename, contentType string, data []byte) response {
	return c.upload("/api/wiki/images", "image", filename, contentType, data, nil)
}

func TestWikiPicturesAreRedrawnServedToMembersAndCheckedAtTheDoor(t *testing.T) {
	f := newWikiFixture(t)
	res := f.uploadImage(f.editor, "big.png", "image/png", bannerPNG(t, 3200, 800))
	wantStatus(t, "upload", res, http.StatusCreated)
	var img wiki.Image
	res.decode(t, &img)
	if !strings.HasPrefix(img.URL, "/api/media/") || img.Markdown != "![]("+img.URL+")" {
		t.Fatalf("image: %+v", img)
	}

	// Every member loads it, a reader included, and it was shrunk to 1600 wide.
	got := f.viewer.get(img.URL)
	wantStatus(t, "viewer loads", got, http.StatusOK)
	cfg, _, err := image.DecodeConfig(bytes.NewReader(got.body))
	if err != nil || cfg.Width != 1600 || cfg.Height != 400 {
		t.Fatalf("size %+v, %v", cfg, err)
	}

	// Metadata does not survive.
	res = f.uploadImage(f.editor, "photo.jpg", "image/jpeg", bannerJPEGWithEXIF(t, "GPS-secret-place"))
	wantStatus(t, "jpeg", res, http.StatusCreated)
	var photo wiki.Image
	res.decode(t, &photo)
	if bytes.Contains(f.viewer.get(photo.URL).body, []byte("GPS-secret-place")) {
		t.Fatal("the EXIF block was kept")
	}

	// Who may upload, and what.
	wantStatus(t, "reader", f.uploadImage(f.viewer, "a.png", "image/png", bannerPNG(t, 10, 10)), http.StatusForbidden)
	solo := f.a.register(f.admin, "solo")
	assignRoles(t, f.admin, solo, createRole(t, f.admin, "manage only", perm.ManageWiki))
	wantStatus(t, "manager without edit", f.uploadImage(solo, "a.png", "image/png", bannerPNG(t, 10, 10)), http.StatusForbidden)
	wantStatus(t, "svg", f.uploadImage(f.editor, "a.svg", "image/svg+xml", []byte(`<svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"/>`)), http.StatusUnsupportedMediaType)
	wantStatus(t, "html as png", f.uploadImage(f.editor, "a.png", "image/png", []byte("<script>alert(1)</script>")), http.StatusUnsupportedMediaType)
	wantStatus(t, "empty", f.uploadImage(f.editor, "a.png", "image/png", nil), http.StatusBadRequest)
	if f.a.store.Len() != 2 {
		t.Fatalf("a refused upload left an object: %d", f.a.store.Len())
	}

	// The audit log knows who added it.
	var n int
	if err := f.a.db.QueryRow(context.Background(), `SELECT count(*) FROM audit_log WHERE action = 'wiki.image'`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("audit entries: %d, %v", n, err)
	}
}

func TestAPictureUploadIsAnEditForTheTimeout(t *testing.T) {
	f := newWikiFixture(t)
	wantStatus(t, "timeout", f.admin.post("/api/admin/users/"+f.editor.user.ID.String()+"/timeout", map[string]any{"duration_seconds": 3600, "reason": "calm"}), http.StatusOK)
	wantStatus(t, "timed out", f.uploadImage(f.editor, "a.png", "image/png", bannerPNG(t, 10, 10)), http.StatusForbidden)
}

func TestAtTheCapUnusedPicturesMakeRoomAndUsedOnesStay(t *testing.T) {
	f := newWikiFixture(t)
	ctx := context.Background()
	var ids []string
	for range 3 {
		res := f.uploadImage(f.admin, "a.png", "image/png", bannerPNG(t, 20, 20))
		wantStatus(t, "upload", res, http.StatusCreated)
		ids = append(ids, imageID(t, res))
	}
	// A page uses the first picture.
	wantStatus(t, "page", f.admin.post("/api/wiki/pages", map[string]any{"title": "Maps", "content": "![](/api/media/" + ids[0] + ")"}), http.StatusCreated)
	// Fill the rest of the cap with old unused rows, then upload again.
	if _, err := f.a.db.Exec(ctx, `UPDATE wiki_images SET created_at = NOW() - interval '3 days'`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.a.db.Exec(ctx, `
		INSERT INTO media (id, uploader_id, s3_bucket, s3_key, original_filename, mime_type, size_bytes)
		SELECT gen_random_uuid(), $1, 'b', 'wiki/fill-' || g, 'w.png', 'image/png', 1 FROM generate_series(1, $2) g`, f.admin.user.ID, wiki.MaxImages-3); err != nil {
		t.Fatal(err)
	}
	if _, err := f.a.db.Exec(ctx, `INSERT INTO wiki_images (media_id, created_at) SELECT id, NOW() - interval '3 days' FROM media WHERE s3_key LIKE 'wiki/fill-%'`); err != nil {
		t.Fatal(err)
	}
	res := f.uploadImage(f.admin, "new.png", "image/png", bannerPNG(t, 20, 20))
	wantStatus(t, "room made", res, http.StatusCreated)
	var used bool
	if err := f.a.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM wiki_images WHERE media_id = $1)`, ids[0]).Scan(&used); err != nil || !used {
		t.Fatalf("the picture a page uses stays: %v %v", used, err)
	}
	var count int
	if err := f.a.db.QueryRow(ctx, `SELECT count(*) FROM wiki_images`).Scan(&count); err != nil || count > wiki.MaxImages {
		t.Fatalf("pictures: %d, %v", count, err)
	}

	// Back at the cap with pictures that are all new: they may still be in a draft, so nothing is removed.
	if _, err := f.a.db.Exec(ctx, `UPDATE wiki_images SET created_at = NOW()`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.a.db.Exec(ctx, `
		INSERT INTO media (id, uploader_id, s3_bucket, s3_key, original_filename, mime_type, size_bytes)
		SELECT gen_random_uuid(), $1, 'b', 'wiki/top-' || g, 'w.png', 'image/png', 1 FROM generate_series(1, $2) g`, f.admin.user.ID, wiki.MaxImages-count); err != nil {
		t.Fatal(err)
	}
	if _, err := f.a.db.Exec(ctx, `INSERT INTO wiki_images (media_id) SELECT id FROM media WHERE s3_key LIKE 'wiki/top-%'`); err != nil {
		t.Fatal(err)
	}
	wantStatus(t, "full", f.uploadImage(f.admin, "late.png", "image/png", bannerPNG(t, 20, 20)), http.StatusConflict)
}

func TestErasingAnUploaderKeepsThePicturesPagesStillShowAndTakesTheRest(t *testing.T) {
	a, _ := mailApp(t)
	admin := a.seedAdmin()
	author := a.register(admin, "wanda")
	reader := a.register(admin, "rita")
	assignRoles(t, admin, author, createRole(t, admin, "wiki editors", perm.EditWiki))
	var used, spare wiki.Image
	res := author.upload("/api/wiki/images", "image", "me.png", "image/png", bannerPNG(t, 30, 30), nil)
	wantStatus(t, "upload", res, http.StatusCreated)
	res.decode(t, &used)
	res = author.upload("/api/wiki/images", "image", "spare.png", "image/png", bannerPNG(t, 31, 31), nil)
	wantStatus(t, "upload spare", res, http.StatusCreated)
	res.decode(t, &spare)
	wantStatus(t, "page", author.post("/api/wiki/pages", map[string]any{"title": "Faces", "content": "Me: " + used.Markdown}), http.StatusCreated)
	a.setEmail(author, "wanda@example.org")
	wantStatus(t, "erase", a.deleteAccount(author, memberPassword, ""), http.StatusNoContent)

	if a.store.Len() != 1 {
		t.Fatalf("only the picture no page shows is gone: %d objects", a.store.Len())
	}
	var content string
	if err := a.db.QueryRow(context.Background(), `SELECT content FROM wiki_pages WHERE slug = 'faces'`).Scan(&content); err != nil || !strings.Contains(content, "Me:") {
		t.Fatalf("the text stays: %q %v", content, err)
	}
	wantStatus(t, "the picture of the page still loads for other members", reader.get(used.URL), http.StatusOK)
	wantStatus(t, "the spare one is gone", admin.get(spare.URL), http.StatusNotFound)
}

func TestWikiPicturesAreReadableOnlyWhereTheWikiIs(t *testing.T) {
	f := newWikiFixture(t)
	var img wiki.Image
	res := f.uploadImage(f.editor, "a.png", "image/png", bannerPNG(t, 20, 20))
	wantStatus(t, "upload", res, http.StatusCreated)
	res.decode(t, &img)
	wantStatus(t, "open wiki", f.viewer.get(img.URL), http.StatusOK)

	everyone := everyoneID(t, f.admin)
	closed := f.admin.patch("/api/admin/roles/"+everyone.String(), map[string]any{"permissions": int64(perm.DefaultEveryone &^ perm.ViewWiki)})
	wantStatus(t, "close the wiki", closed, http.StatusOK)
	wantStatus(t, "closed wiki hides the picture", f.viewer.get(img.URL), http.StatusNotFound)
	wantStatus(t, "an administrator still sees it", f.admin.get(img.URL), http.StatusOK)
}

func imageID(t *testing.T, res response) string {
	t.Helper()
	var img wiki.Image
	res.decode(t, &img)
	return img.ID.String()
}
