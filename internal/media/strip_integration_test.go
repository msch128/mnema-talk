//go:build integration

package media

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	_ "image/jpeg"
	"io"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/msch128/mnema-talk/internal/perm"
)

// What the fixture photo (internal/imgstrip/testdata) carries: the camera
// make sits in the EXIF record next to a GPS position, the other mark in XMP.
const (
	photoEXIFMark = "GPS-FIXTURE-CAM"
	photoXMPMark  = "xmp-fixture-location"
	// photoTurned is the one EXIF record a stored photo keeps: orientation 6.
	photoTurned = "Exif\x00\x00MM\x00\x2a\x00\x00\x00\x08\x00\x01\x01\x12\x00\x03\x00\x00\x00\x01\x00\x06\x00\x00\x00\x00\x00\x00"
)

func fixturePhoto(t *testing.T) []byte {
	t.Helper()
	photo, err := os.ReadFile("../imgstrip/testdata/phone-orientation-6.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(photo, []byte(photoEXIFMark)) || !bytes.Contains(photo, []byte(photoXMPMark)) {
		t.Fatal("the fixture photo has no metadata")
	}
	return photo
}

// storedUploadOf uploads data as a channel attachment and returns the object
// in storage and the size recorded for it.
func storedUploadOf(t *testing.T, f *mediaFixture, filename, mimeType string, data []byte) (object []byte, recorded int64) {
	t.Helper()
	f.h.MaxUploadBytes = 1 << 20
	req := f.req(uploadRequest(t, formPart{name: "file", filename: filename, mime: mimeType, value: string(data)}), "channelID", f.channel.String())
	if err := f.h.upload(httptest.NewRecorder(), req); err != nil {
		t.Fatalf("upload %s: %v", filename, err)
	}
	var key string
	if err := f.p.QueryRow(context.Background(), `SELECT s3_key, size_bytes FROM media WHERE original_filename = $1`, filename).Scan(&key, &recorded); err != nil {
		t.Fatal(err)
	}
	body, err := f.store.GetObjectFrom(context.Background(), key, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close()
	if object, err = io.ReadAll(body); err != nil {
		t.Fatal(err)
	}
	return object, recorded
}

func decoded(t *testing.T, data []byte) image.Image {
	t.Helper()
	m, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// A photo sent as an attachment is stored without its GPS position, camera
// and XMP, shows the same picture turned the same way, and its stored size is
// the one that is recorded and counted.
func TestAPhotoAttachmentIsStoredWithoutItsMetadata(t *testing.T) {
	f := mediaDB(t)
	photo := fixturePhoto(t)
	object, recorded := storedUploadOf(t, f, "holiday.jpg", "image/jpeg", photo)

	for _, mark := range []string{photoEXIFMark, photoXMPMark, "GPS", "xmpmeta"} {
		if bytes.Contains(object, []byte(mark)) {
			t.Errorf("the stored photo still holds %q", mark)
		}
	}
	if n := bytes.Count(object, []byte("Exif\x00\x00")); n != 1 || !bytes.Contains(object, []byte(photoTurned)) {
		t.Errorf("the stored photo has %d EXIF records, want one that holds orientation 6 only", n)
	}
	before, after := decoded(t, photo), decoded(t, object)
	if before.Bounds() != after.Bounds() {
		t.Fatalf("picture %v became %v", before.Bounds(), after.Bounds())
	}
	for y := range before.Bounds().Dy() {
		for x := range before.Bounds().Dx() {
			if before.At(x, y) != after.At(x, y) {
				t.Fatalf("pixel %d,%d changed", x, y)
			}
		}
	}

	if len(object) >= len(photo) || recorded != int64(len(object)) {
		t.Errorf("sent %d bytes, stored %d, recorded %d", len(photo), len(object), recorded)
	}
	var counted int64
	if err := f.p.QueryRow(context.Background(), `SELECT SUM(bytes)::bigint FROM upload_usage WHERE user_id = $1`, f.user.ID).Scan(&counted); err != nil || counted != int64(len(object)) {
		t.Errorf("counted %d bytes against the quota, stored %d (%v)", counted, len(object), err)
	}
}

// A file that is not a JPEG, PNG or WebP is stored byte for byte, also when
// it holds what looks like a photo's metadata.
func TestOtherAttachmentsAreStoredByteForByte(t *testing.T) {
	f := mediaDB(t)
	photo := fixturePhoto(t)
	files := map[string]struct {
		mime string
		data []byte
	}{
		"notes.txt":   {"text/plain", []byte("Exif notes about " + photoEXIFMark)},
		"paper.pdf":   {"application/pdf", append([]byte("%PDF-1.7\n"), photo...)},
		"clip.gif":    {"image/gif", append([]byte("GIF89a\x01\x00\x01\x00\x00\x00\x00;"), photo[2:]...)},
		"archive.bin": {"application/octet-stream", append([]byte{0, 1, 2, 3}, photo...)},
	}
	for name, file := range files {
		object, recorded := storedUploadOf(t, f, name, file.mime, file.data)
		if !bytes.Equal(object, file.data) || recorded != int64(len(file.data)) {
			t.Errorf("%s: sent %d bytes, stored %d, recorded %d", name, len(file.data), len(object), recorded)
		}
	}
}

// The Owner can switch the removal off; photos are then stored as sent.
func TestMetadataStaysWhenTheOwnerSwitchedTheRemovalOff(t *testing.T) {
	f := mediaDB(t)
	f.h.Perms = perm.NewService(f.p)
	photo := fixturePhoto(t)
	off := false
	settings, err := f.h.Perms.SetUploadSettings(context.Background(), f.user.Subject(), nil, &off)
	if err != nil || settings.StripImageMetadata {
		t.Fatalf("switching the removal off: %+v %v", settings, err)
	}
	if object, _ := storedUploadOf(t, f, "kept.jpg", "image/jpeg", photo); !bytes.Equal(object, photo) {
		t.Error("the photo was changed although the removal is off")
	}

	// Leaving the switch out of a change keeps it; switching it on again strips.
	if settings, err = f.h.Perms.SetUploadSettings(context.Background(), f.user.Subject(), nil, nil); err != nil || settings.StripImageMetadata {
		t.Fatalf("a change without the switch: %+v %v", settings, err)
	}
	on := true
	if _, err := f.h.Perms.SetUploadSettings(context.Background(), f.user.Subject(), nil, &on); err != nil {
		t.Fatal(err)
	}
	if object, _ := storedUploadOf(t, f, "stripped.jpg", "image/jpeg", photo); bytes.Contains(object, []byte(photoEXIFMark)) {
		t.Error("the photo kept its metadata although the removal is on again")
	}
}

// A file added in the composer goes the same way, and the size the client is
// told is the stored one.
func TestAStagedPhotoIsStoredWithoutItsMetadata(t *testing.T) {
	f := mediaDB(t)
	f.h.MaxUploadBytes = 1 << 20
	photo := fixturePhoto(t)
	w := httptest.NewRecorder()
	req := f.req(uploadRequest(t, formPart{name: "file", filename: "staged.jpg", mime: "image/jpeg", value: string(photo)}), "channelID", f.channel.String())
	if err := f.h.stageAttachment(w, req); err != nil {
		t.Fatal(err)
	}
	var staged struct {
		Size int64 `json:"size_bytes"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &staged); err != nil {
		t.Fatal(err)
	}
	var key string
	if err := f.p.QueryRow(context.Background(), `SELECT s3_key FROM media WHERE staged`).Scan(&key); err != nil {
		t.Fatal(err)
	}
	body, err := f.store.GetObjectFrom(context.Background(), key, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close()
	object, _ := io.ReadAll(body)
	if bytes.Contains(object, []byte(photoEXIFMark)) || staged.Size != int64(len(object)) || len(object) >= len(photo) {
		t.Errorf("sent %d bytes, stored %d, client told %d", len(photo), len(object), staged.Size)
	}
}

// The upload limit holds for what is stored: a photo exactly at the limit is
// accepted and its stored file is within it, also when the photo came with
// the smallest EXIF record there is and with a second picture behind it.
func TestAStoredPhotoStaysWithinTheUploadLimit(t *testing.T) {
	f := mediaDB(t)
	photo := fixturePhoto(t)
	smallest := "\xff\xe1\x00\x1eExif\x00\x00II\x2a\x00\x08\x00\x00\x00\x01\x00\x12\x01\x03\x00\x01\x00\x00\x00\x06\x00\x00\x00"
	plain := photo[bytes.Index(photo, []byte("\xff\xdb")):]
	turned := append([]byte("\xff\xd8"+smallest), plain...)
	sent := append(bytes.Clone(turned), photo...)
	limit := int64(len(sent))

	f.h.MaxUploadBytes = limit
	req := f.req(uploadRequest(t, formPart{name: "file", filename: "limit.jpg", mime: "image/jpeg", value: string(sent)}), "channelID", f.channel.String())
	if err := f.h.upload(httptest.NewRecorder(), req); err != nil {
		t.Fatalf("a photo at the limit: %v", err)
	}
	var key string
	var recorded int64
	if err := f.p.QueryRow(context.Background(), `SELECT s3_key, size_bytes FROM media WHERE original_filename = 'limit.jpg'`).Scan(&key, &recorded); err != nil {
		t.Fatal(err)
	}
	body, err := f.store.GetObjectFrom(context.Background(), key, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close()
	object, _ := io.ReadAll(body)
	if len(object) != len(turned) || !bytes.HasSuffix(object, plain) || recorded != int64(len(object)) || recorded > limit {
		t.Errorf("sent %d bytes with a limit of %d, stored %d, recorded %d", len(sent), limit, len(object), recorded)
	}
	if bytes.Contains(object, []byte(photoEXIFMark)) {
		t.Error("the picture behind the photo was stored with its metadata")
	}

	// One byte more than the limit is refused, whatever would be stored of it.
	f.h.MaxUploadBytes = limit - 1
	req = f.req(uploadRequest(t, formPart{name: "file", filename: "over.jpg", mime: "image/jpeg", value: string(sent)}), "channelID", f.channel.String())
	if err := f.h.upload(httptest.NewRecorder(), req); err == nil {
		t.Error("a photo above the limit was accepted because its tail is not stored")
	}
}
