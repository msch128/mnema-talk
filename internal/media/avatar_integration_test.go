//go:build integration

package media

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAvatarStorageAndDatabaseFailures(t *testing.T) {
	f := mediaDB(t)
	request := func() *http.Request {
		return f.req(uploadRequest(t, formPart{name: "avatar", filename: "a.png", mime: "image/png", value: bannerPNG(t, 8, 8)}), "", "")
	}
	f.store.uploadErr = errors.New("store rejected upload")
	if err := f.h.uploadAvatar(httptest.NewRecorder(), request()); err == nil || !strings.Contains(err.Error(), "store avatar") {
		t.Fatalf("storage failure=%v", err)
	}
	f.store.uploadErr = nil
	f.store.afterUpload = func() { execMedia(t, f.p, `DELETE FROM users WHERE id=$1`, f.user.ID) }
	if err := f.h.uploadAvatar(httptest.NewRecorder(), request()); err == nil || !strings.Contains(err.Error(), "record avatar") || f.store.Len() != 0 {
		t.Fatalf("missing avatar user=%v objects=%d", err, f.store.Len())
	}
}

func TestAvatarTransactionRollsBackRejectedMediaInsert(t *testing.T) {
	f := mediaDB(t)
	execMedia(t, f.p, `CREATE FUNCTION reject_test_media() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'media write rejected'; END $$`)
	execMedia(t, f.p, `CREATE TRIGGER reject_test_media BEFORE INSERT ON media FOR EACH ROW EXECUTE FUNCTION reject_test_media()`)
	t.Cleanup(func() {
		execMedia(t, f.p, `DROP TRIGGER reject_test_media ON media`)
		execMedia(t, f.p, `DROP FUNCTION reject_test_media()`)
	})
	r := f.req(uploadRequest(t, formPart{name: "avatar", filename: "a.png", mime: "image/png", value: bannerPNG(t, 8, 8)}), "", "")
	if err := f.h.uploadAvatar(httptest.NewRecorder(), r); err == nil || !strings.Contains(err.Error(), "media write rejected") || f.store.Len() != 0 {
		t.Fatalf("rejected media insert=%v objects=%d", err, f.store.Len())
	}
	var previous *string
	if err := f.p.QueryRow(context.Background(), `SELECT avatar_s3_key FROM users WHERE id=$1`, f.user.ID).Scan(&previous); err != nil || previous != nil {
		t.Fatalf("rolled-back avatar=%v %v", previous, err)
	}
}

func TestAvatarReplacementDeleteFailureDoesNotUndoNewAvatar(t *testing.T) {
	f := mediaDB(t)
	old := f.attachment(t, "avatars/old.png", true)
	execMedia(t, f.p, `UPDATE users SET avatar_s3_key=$1 WHERE id=$2`, old.String(), f.user.ID)
	f.store.deleteErr = errors.New("old object delete unavailable")
	r := f.req(uploadRequest(t, formPart{name: "avatar", filename: "a.png", mime: "image/png", value: bannerPNG(t, 8, 8)}), "", "")
	w := httptest.NewRecorder()
	if err := f.h.uploadAvatar(w, r); err != nil || w.Code != 200 || f.store.Len() != 2 {
		t.Fatalf("replacement=%v code=%d objects=%d", err, w.Code, f.store.Len())
	}
	f.store.deleteErr = nil
	// Deleting the account concurrently with cleanup must be reported when reloading the updated user.
	f.store.afterDelete = func() { execMedia(t, f.p, `DELETE FROM users WHERE id=$1`, f.user.ID) }
	r = f.req(uploadRequest(t, formPart{name: "avatar", filename: "b.png", mime: "image/png", value: bannerPNG(t, 8, 8)}), "", "")
	if err := f.h.uploadAvatar(httptest.NewRecorder(), r); err == nil {
		t.Fatal("missing updated user was not reported")
	}
}
