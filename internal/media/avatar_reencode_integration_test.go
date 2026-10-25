//go:build integration

package media

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/events"
)

const exifMarker = "camera-model-and-place-in-exif"

// jpegWithEXIF is a JPEG whose second segment is an EXIF block.
func jpegWithEXIF(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewNRGBA(image.Rect(0, 0, 800, 600)), nil); err != nil {
		t.Fatal(err)
	}
	payload := append([]byte("Exif\x00\x00"), []byte(exifMarker)...)
	seg := append([]byte{0xFF, 0xE1, byte((len(payload) + 2) >> 8), byte(len(payload) + 2)}, payload...)
	raw := buf.Bytes()
	return append(append(append([]byte{}, raw[:2]...), seg...), raw[2:]...)
}

// oldAvatar stores data the way 1.0 did: the file as uploaded, not marked.
func (f *mediaFixture) oldAvatar(t *testing.T, data []byte, mime string) (uuid.UUID, string) {
	t.Helper()
	id := uuid.New()
	key := "avatars/2026/01/" + id.String() + ".jpg"
	f.store.Put(key, data, time.Now().Add(-72*time.Hour))
	execMedia(t, f.p, `INSERT INTO media(id,uploader_id,s3_bucket,s3_key,original_filename,mime_type,size_bytes) VALUES($1,$2,'test-media',$3,'me.jpg',$4,$5)`, id, f.user.ID, key, mime, len(data))
	execMedia(t, f.p, `UPDATE users SET avatar_s3_key=$1, avatar_reencoded_at=NULL WHERE id=$2`, id.String(), f.user.ID)
	return id, key
}

func (f *mediaFixture) avatarState(t *testing.T) (key string, marked bool) {
	t.Helper()
	var media string
	if err := f.p.QueryRow(context.Background(), `SELECT avatar_s3_key, avatar_reencoded_at IS NOT NULL FROM users WHERE id=$1`, f.user.ID).Scan(&media, &marked); err != nil {
		t.Fatal(err)
	}
	if err := f.p.QueryRow(context.Background(), `SELECT s3_key FROM media WHERE id=$1::uuid`, media).Scan(&key); err != nil {
		t.Fatal(err)
	}
	return key, marked
}

func TestOldAvatarsLoseTheirMetadataOnceAfterTheUpdate(t *testing.T) {
	f := mediaDB(t)
	_, oldKey := f.oldAvatar(t, jpegWithEXIF(t), "image/jpeg")

	done, failed, err := f.h.ReencodeAvatars(context.Background())
	if err != nil || done != 1 || failed != 0 {
		t.Fatalf("job: done=%d failed=%d err=%v", done, failed, err)
	}
	f.wantUserUpdate(t)
	newKey, marked := f.avatarState(t)
	if newKey == oldKey || !marked {
		t.Fatalf("the account points at the redrawn file and is marked: %s marked=%v", newKey, marked)
	}
	if f.store.Has(oldKey) || f.store.Len() != 1 {
		t.Fatalf("the old object is deleted after the new one is written: %d objects", f.store.Len())
	}
	body, err := f.store.GetObjectFrom(context.Background(), newKey, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close()
	var got bytes.Buffer
	_, _ = got.ReadFrom(body)
	if bytes.Contains(got.Bytes(), []byte(exifMarker)) || bytes.Contains(got.Bytes(), []byte("Exif")) {
		t.Fatal("EXIF survived the re-encode")
	}
	if cfg, err := jpeg.DecodeConfig(&got); err != nil || cfg.Width != 512 || cfg.Height != 512 {
		t.Fatalf("redrawn as a 512 square: %v %v", cfg, err)
	}

	if done, failed, err := f.h.ReencodeAvatars(context.Background()); err != nil || done != 0 || failed != 0 {
		t.Fatalf("it never runs twice for the same avatar: done=%d failed=%d err=%v", done, failed, err)
	}
	if again, _ := f.avatarState(t); again != newKey {
		t.Fatal("a second run changed the avatar")
	}
}

func (f *mediaFixture) avatarKey(t *testing.T) *string {
	t.Helper()
	var key *string
	if err := f.p.QueryRow(context.Background(), `SELECT avatar_s3_key FROM users WHERE id=$1`, f.user.ID).Scan(&key); err != nil {
		t.Fatal(err)
	}
	return key
}

func TestOldAvatarOutsideTodaysLimitsIsStillRedrawn(t *testing.T) {
	f := mediaDB(t)
	// 8200 pixels wide was fine in 1.0 and is over the limit for new uploads.
	var wide bytes.Buffer
	if err := png.Encode(&wide, image.NewNRGBA(image.Rect(0, 0, 8200, 40))); err != nil {
		t.Fatal(err)
	}
	_, oldKey := f.oldAvatar(t, wide.Bytes(), "image/png")
	done, failed, err := f.h.ReencodeAvatars(context.Background())
	if err != nil || done != 1 || failed != 0 {
		t.Fatalf("job: done=%d failed=%d err=%v", done, failed, err)
	}
	key, marked := f.avatarState(t)
	if key == oldKey || !marked || f.store.Has(oldKey) {
		t.Fatalf("redrawn, marked, old file gone: %s %v", key, marked)
	}
	body, err := f.store.GetObjectFrom(context.Background(), key, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close()
	if cfg, err := png.DecodeConfig(body); err != nil || cfg.Width != 40 || cfg.Height != 40 {
		t.Fatalf("centre square: %v %v", cfg, err)
	}
}

func TestOldAvatarThatCannotBeReadAtAllIsRemoved(t *testing.T) {
	f := mediaDB(t)
	f.h.Audit = &audit.Log{DB: f.p}
	// A real EXIF block around a body no decoder understands.
	broken := append(jpegWithEXIF(t)[:200:200], bytes.Repeat([]byte{0xAB}, 64)...)
	_, key := f.oldAvatar(t, broken, "image/jpeg")
	done, failed, err := f.h.ReencodeAvatars(context.Background())
	if err != nil || done != 1 || failed != 0 {
		t.Fatalf("job: done=%d failed=%d err=%v", done, failed, err)
	}
	if f.avatarKey(t) != nil || f.store.Has(key) || f.store.Len() != 0 {
		t.Fatal("the avatar is removed, never kept with its metadata")
	}
	var action, reason string
	if err := f.p.QueryRow(context.Background(), `SELECT action, details->>'reason' FROM audit_log ORDER BY id DESC LIMIT 1`).Scan(&action, &reason); err != nil || action != "media.avatar_removed" || reason == "" {
		t.Fatalf("audited with a reason: %q %q %v", action, reason, err)
	}
	f.wantUserUpdate(t)
	if done, _, _ := f.h.ReencodeAvatars(context.Background()); done != 0 {
		t.Fatal("nothing left to do")
	}
}

// wantUserUpdate checks the job announced the owner's new profile, as a
// manual change does.
func (f *mediaFixture) wantUserUpdate(t *testing.T) {
	t.Helper()
	sent := f.h.Events.(*events.Recorder).Snapshot()
	if len(sent) != 1 || sent[0].Type != events.UserUpdate {
		t.Fatalf("want one user_update, got %+v", sent)
	}
	if u, ok := sent[0].Payload.(auth.User); !ok || u.ID != f.user.ID {
		t.Fatalf("payload is the owner's public profile: %#v", sent[0].Payload)
	}
}

func TestStorageTroubleLeavesTheAvatarForTheNextRun(t *testing.T) {
	f := mediaDB(t)
	_, key := f.oldAvatar(t, jpegWithEXIF(t), "image/jpeg")
	f.h.Store = getFailureStore{Store: f.store, err: errors.New("storage unavailable")}
	done, failed, err := f.h.ReencodeAvatars(context.Background())
	if err != nil || done != 0 || failed != 1 {
		t.Fatalf("job: done=%d failed=%d err=%v", done, failed, err)
	}
	if got, marked := f.avatarState(t); got != key || marked {
		t.Fatal("not marked, so the next run tries again")
	}
	f.h.Store = f.store
	if done, _, _ := f.h.ReencodeAvatars(context.Background()); done != 1 {
		t.Fatal("the retry succeeds")
	}
}

func TestAvatarChangedWhileTheJobRanIsLeftAlone(t *testing.T) {
	f := mediaDB(t)
	id, _ := f.oldAvatar(t, jpegWithEXIF(t), "image/jpeg")
	newID, newKey := f.oldAvatar(t, jpegWithEXIF(t), "image/jpeg") // the member's newer avatar
	stale := storedAvatar{userID: f.user.ID, mediaID: id.String(), key: "avatars/2026/01/" + id.String() + ".jpg"}
	if err := f.h.reencodeAvatar(context.Background(), stale); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.avatarState(t); got != newKey || !f.store.Has(newKey) {
		t.Fatalf("the newer avatar %s stays: %s", newID, got)
	}
	if f.store.Len() != 2 {
		t.Fatalf("the redrawn copy of the stale one is discarded: %d objects", f.store.Len())
	}
}
