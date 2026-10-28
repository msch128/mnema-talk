//go:build integration

package server

import (
	"bytes"
	"image"
	"image/png"
	"math/rand"
	"net/http"
	"testing"
)

// noisePNG is an incompressible PNG of about 2 MB: above the 1 MiB JSON body
// limit, below the 5 MB image caps.
func noisePNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 800, 800))
	rng := rand.New(rand.NewSource(1))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = byte(rng.Intn(256)), byte(rng.Intn(256)), byte(rng.Intn(256)), 255
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if buf.Len() <= 1<<20 {
		t.Fatalf("test image is only %d bytes", buf.Len())
	}
	return buf.Bytes()
}

// Admin-area and event image uploads are documented up to 5 MB, so the JSON
// body limit must not apply to them.
func TestImageUploadsAboveJSONBodyLimit(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	data := noisePNG(t)

	ev := a.coverEvent(admin, nil)
	wantStatus(t, "event cover of 2 MB", a.uploadCover(admin, ev.ID, "cover", data), http.StatusOK)

	ch := a.createChannel(admin, "alerts", "text")
	h := makeHook(t, admin, ch, "bot")
	res := admin.upload("/api/admin/webhooks/"+h.ID.String()+"/avatar", "avatar", "a.png", "image/png", data, nil)
	wantStatus(t, "webhook avatar of 2 MB", res, http.StatusOK)

	b := createBot(t, admin, "pic")
	res = admin.upload("/api/admin/bots/"+b.ID.String()+"/avatar", "avatar", "a.png", "image/png", data, nil)
	wantStatus(t, "bot avatar of 2 MB", res, http.StatusOK)
}
