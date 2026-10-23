//go:build integration

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"testing"
)

const bannerRoute = "/api/users/me/banner"

func bannerPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = 30, 160, 120, 255
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// bannerJPEGWithEXIF is a JPEG whose second segment is an EXIF block naming a
// camera and a place.
func bannerJPEGWithEXIF(t *testing.T, marker string) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewNRGBA(image.Rect(0, 0, 900, 300)), nil); err != nil {
		t.Fatal(err)
	}
	payload := append([]byte("Exif\x00\x00"), []byte(marker)...)
	seg := append([]byte{0xFF, 0xE1, byte((len(payload) + 2) >> 8), byte(len(payload) + 2)}, payload...)
	raw := buf.Bytes()
	return append(append(append([]byte{}, raw[:2]...), seg...), raw[2:]...)
}

func bannerGIF(t *testing.T, w, h, frames int) []byte {
	t.Helper()
	g := &gif.GIF{}
	for range frames {
		g.Image = append(g.Image, image.NewPaletted(image.Rect(0, 0, w, h), color.Palette{color.Black, color.White}))
		g.Delay = append(g.Delay, 5)
	}
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, g); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// putBanner sends a multipart banner with PUT.
func putBanner(c *client, filename, contentType string, data []byte) response {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", `form-data; name="banner"; filename="`+filename+`"`)
	h.Set("Content-Type", contentType)
	part, _ := mw.CreatePart(h)
	_, _ = part.Write(data)
	_ = mw.Close()
	return c.do(http.MethodPut, bannerRoute, &buf, map[string]string{"Content-Type": mw.FormDataContentType()})
}

func profileOf(t *testing.T, viewer, who *client) map[string]any {
	t.Helper()
	res := viewer.get("/api/users/" + who.user.ID.String())
	if res.status != http.StatusOK {
		t.Fatalf("profile: %d %s", res.status, res.body)
	}
	var out map[string]any
	res.decode(t, &out)
	return out
}

func TestBannerIsSetShownReplacedAndRemoved(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	alice := a.register(admin, "alice")
	bob := a.register(admin, "bob")

	if profileOf(t, bob, alice)["banner_url"] != nil {
		t.Fatal("no banner yet")
	}
	res := putBanner(alice, "wide.png", "image/png", bannerPNG(t, 3000, 600))
	if res.status != http.StatusOK {
		t.Fatalf("set banner: %d %s", res.status, res.body)
	}
	var set struct {
		BannerURL string `json:"banner_url"`
	}
	res.decode(t, &set)
	if !strings.HasPrefix(set.BannerURL, "/api/media/") {
		t.Fatalf("banner_url: %q", set.BannerURL)
	}

	// Another member sees it on the profile, served as a 1500x500 PNG.
	if profileOf(t, bob, alice)["banner_url"] != set.BannerURL || profileOf(t, alice, alice)["banner_url"] != set.BannerURL {
		t.Fatal("the banner belongs on every view of the profile")
	}
	served := bob.get(set.BannerURL)
	cfg, format, err := image.DecodeConfig(bytes.NewReader(served.body))
	if served.status != http.StatusOK || err != nil || format != "png" || cfg.Width != 1500 || cfg.Height != 500 {
		t.Fatalf("served banner: %d %v %s %dx%d", served.status, err, format, cfg.Width, cfg.Height)
	}
	if !strings.Contains(served.header.Get("Cache-Control"), "immutable") || served.header.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("banners are cached like avatars and served nosniff: %v", served.header)
	}
	if me := alice.get("/api/auth/me"); !strings.Contains(string(me.body), set.BannerURL) {
		t.Errorf("own account carries the banner: %s", me.body)
	}

	// Not in the member list or in a message.
	ch := a.createChannel(admin, "general", "text")
	a.send(alice, ch, "hello")
	if list := bob.get("/api/channels/" + ch.String() + "/messages"); strings.Contains(string(list.body), "banner") {
		t.Errorf("messages do not carry banners: %s", list.body)
	}

	// Replacing deletes the old object.
	before := a.store.Len()
	if r := putBanner(alice, "second.png", "image/png", bannerPNG(t, 900, 300)); r.status != http.StatusOK {
		t.Fatalf("replace: %d %s", r.status, r.body)
	}
	if a.store.Len() != before {
		t.Fatalf("the old banner file must be gone: %d objects, had %d", a.store.Len(), before)
	}
	if old := bob.get(set.BannerURL); old.status != http.StatusGone {
		t.Errorf("replaced banner: %d", old.status)
	}

	// Removing deletes the object and the reference; removing again is fine.
	if r := alice.delete(bannerRoute); r.status != http.StatusOK || strings.Contains(string(r.body), "banner_url") {
		t.Fatalf("remove: %d %s", r.status, r.body)
	}
	if a.store.Len() != before-1 || profileOf(t, bob, alice)["banner_url"] != nil {
		t.Fatalf("removed banner is gone: %d objects", a.store.Len())
	}
	if r := alice.delete(bannerRoute); r.status != http.StatusOK {
		t.Fatalf("remove again: %d", r.status)
	}
	if r := a.anon().do(http.MethodPut, bannerRoute, nil, nil); r.status != http.StatusUnauthorized {
		t.Fatalf("without a session: %d", r.status)
	}
}

func TestBannerUploadLimitsAndTypes(t *testing.T) {
	a := newApp(t, false)
	alice := a.register(a.seedAdmin(), "alice")

	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)
	cases := []struct {
		name, ct string
		data     []byte
		want     int
	}{
		{"svg.svg", "image/svg+xml", svg, http.StatusUnsupportedMediaType},
		{"page.png", "image/png", []byte("<!DOCTYPE html><script>1</script>"), http.StatusUnsupportedMediaType},
		{"small.png", "image/png", bannerPNG(t, 200, 80), http.StatusBadRequest},
		{"huge.png", "image/png", bannerPNG(t, 5000, 4000), http.StatusBadRequest},
		{"long.gif", "image/gif", bannerGIF(t, 600, 600, 50), http.StatusBadRequest},
		{"big.png", "image/png", append(bannerPNG(t, 600, 200), make([]byte, 7<<20)...), http.StatusRequestEntityTooLarge},
	}
	for _, c := range cases {
		if r := putBanner(alice, c.name, c.ct, c.data); r.status != c.want {
			t.Errorf("%s: %d %s, want %d", c.name, r.status, r.body, c.want)
		}
	}
	if r := alice.do(http.MethodPut, bannerRoute, strings.NewReader("x"), map[string]string{"Content-Type": "text/plain"}); r.status != http.StatusBadRequest {
		t.Errorf("no multipart form: %d", r.status)
	}
	if a.store.Len() != 0 || profileOf(t, alice, alice)["banner_url"] != nil {
		t.Fatal("a rejected upload leaves nothing behind")
	}
	if r := putBanner(alice, "ok.gif", "image/gif", bannerGIF(t, 600, 300, 3)); r.status != http.StatusOK {
		t.Fatalf("an animation within the budget: %d %s", r.status, r.body)
	}
}

func TestBannerUploadIsRedrawnWithoutMetadata(t *testing.T) {
	a := newApp(t, false)
	alice := a.register(a.seedAdmin(), "alice")

	const marker = "camera-model-and-place-in-exif"
	res := putBanner(alice, "photo.jpg", "image/jpeg", bannerJPEGWithEXIF(t, marker))
	if res.status != http.StatusOK {
		t.Fatalf("jpeg banner: %d %s", res.status, res.body)
	}
	url := profileOf(t, alice, alice)["banner_url"].(string)
	served := alice.get(url)
	if bytes.Contains(served.body, []byte(marker)) || bytes.Contains(served.body, []byte("Exif")) {
		t.Fatal("EXIF must not survive")
	}
	if served.header.Get("Content-Type") != "image/jpeg" {
		t.Errorf("a JPEG stays a JPEG: %s", served.header.Get("Content-Type"))
	}
	if _, err := jpeg.Decode(bytes.NewReader(served.body)); err != nil {
		t.Fatalf("stored file is a valid JPEG: %v", err)
	}
}

func TestHealthListsTheBannerFeature(t *testing.T) {
	a := newApp(t, false)
	res := a.anon().get("/api/health")
	var h struct {
		Features []string `json:"features"`
	}
	res.decode(t, &h)
	for _, f := range h.Features {
		if f == "profile.banner" {
			return
		}
	}
	t.Fatalf("features: %v", h.Features)
}

func TestBannerIsInTheExportAndGoneAfterErasure(t *testing.T) {
	a, _ := mailApp(t)
	admin := a.seedAdmin()
	alice := a.register(admin, "alice")
	if r := putBanner(alice, "b.png", "image/png", bannerPNG(t, 900, 300)); r.status != http.StatusOK {
		t.Fatalf("banner: %d %s", r.status, r.body)
	}
	if res := a.requestExport(alice, memberPassword, ""); res.status != http.StatusAccepted {
		t.Fatalf("export: %d %s", res.status, res.body)
	}
	if st := a.waitForExport(alice); st.State != "ready" {
		t.Fatalf("export state: %+v", st)
	}
	files := unzipAll(t, alice.get("/api/users/me/export/download").body)
	var profile map[string]any
	if err := json.Unmarshal([]byte(files["profile.json"]), &profile); err != nil || profile["banner_file"] != "banner.png" {
		t.Fatalf("profile.json names the banner: %v %s", err, files["profile.json"])
	}
	if cfg, err := png.DecodeConfig(strings.NewReader(files["banner.png"])); err != nil || cfg.Width != 900 || cfg.Height != 300 {
		t.Fatalf("the banner file is in the archive: %v", err)
	}

	if res := a.deleteAccount(alice, memberPassword, ""); res.status != http.StatusNoContent {
		t.Fatalf("erase: %d %s", res.status, res.body)
	}
	if a.store.Len() != 0 {
		t.Fatalf("the banner file must be erased: %d objects", a.store.Len())
	}
	var banner *string
	if err := a.db.QueryRow(context.Background(), `SELECT banner_s3_key FROM users WHERE id = $1`, alice.user.ID).Scan(&banner); err != nil || banner != nil {
		t.Fatalf("banner reference after erasure: %v %v", banner, err)
	}
}
