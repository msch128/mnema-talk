//go:build integration

package server

import (
	"bytes"
	"image"
	"image/gif"
	"image/jpeg"
	"net/http"
	"strings"
	"testing"
)

const avatarRoute = "/api/users/me/avatar"

func TestAvatarIsRedrawnAsASquareWithoutMetadata(t *testing.T) {
	a := newApp(t, false)
	alice := a.register(a.seedAdmin(), "alice")

	const marker = "camera-model-and-place-in-exif"
	res := alice.upload(avatarRoute, "avatar", "phone.jpg", "image/jpeg", bannerJPEGWithEXIF(t, marker), nil)
	if res.status != http.StatusOK {
		t.Fatalf("avatar: %d %s", res.status, res.body)
	}
	url := profileOf(t, alice, alice)["avatar_url"].(string)
	served := alice.get(url)
	if bytes.Contains(served.body, []byte(marker)) || bytes.Contains(served.body, []byte("Exif")) {
		t.Fatal("EXIF must not survive in an avatar")
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(served.body))
	if err != nil || cfg.Width != 300 || cfg.Height != 300 {
		t.Fatalf("a 900x300 photo becomes its centre square: %v %dx%d", err, cfg.Width, cfg.Height)
	}
	big := alice.upload(avatarRoute, "avatar", "big.png", "image/png", []byte(bannerPNG(t, 1600, 1200)), nil)
	if big.status != http.StatusOK {
		t.Fatalf("large avatar: %d %s", big.status, big.body)
	}
	bigURL := profileOf(t, alice, alice)["avatar_url"].(string)
	if got, _, err := image.DecodeConfig(bytes.NewReader(alice.get(bigURL).body)); err != nil || got.Width != 512 || got.Height != 512 {
		t.Fatalf("shrunk to 512x512: %v %v", got, err)
	}
	if a.store.Len() != 1 {
		t.Fatalf("only the current avatar stays: %d objects", a.store.Len())
	}
}

func TestAnimatedAvatarFollowsTheAnimationBudget(t *testing.T) {
	a := newApp(t, false)
	alice := a.register(a.seedAdmin(), "alice")

	if r := alice.upload(avatarRoute, "avatar", "a.gif", "image/gif", bannerGIF(t, 200, 200, 4), nil); r.status != http.StatusOK {
		t.Fatalf("animated avatar: %d %s", r.status, r.body)
	}
	served := alice.get(profileOf(t, alice, alice)["avatar_url"].(string))
	if g, err := gif.DecodeAll(bytes.NewReader(served.body)); err != nil || len(g.Image) != 4 {
		t.Fatalf("stays animated: %v", err)
	}
	if r := alice.upload(avatarRoute, "avatar", "long.gif", "image/gif", bannerGIF(t, 600, 600, 50), nil); r.status != http.StatusBadRequest {
		t.Errorf("over the pixel budget: %d", r.status)
	}
	if r := alice.upload(avatarRoute, "avatar", "one.gif", "image/gif", bannerGIF(t, 64, 64, 1), nil); r.status != http.StatusOK {
		t.Fatalf("single frame: %d", r.status)
	}
	if got := alice.get(profileOf(t, alice, alice)["avatar_url"].(string)); got.header.Get("Content-Type") != "image/png" {
		t.Errorf("a one-frame GIF becomes a PNG: %s", got.header.Get("Content-Type"))
	}
	animatedWebP := append([]byte("RIFF\x00\x00\x00\x00WEBPVP8X\x0a\x00\x00\x00\x02"), make([]byte, 20)...)
	if r := alice.upload(avatarRoute, "avatar", "a.webp", "image/webp", animatedWebP, nil); r.status != http.StatusBadRequest {
		t.Errorf("animated WebP: %d %s", r.status, r.body)
	}
}

func TestAvatarCanBeRemoved(t *testing.T) {
	a := newApp(t, false)
	alice := a.register(a.seedAdmin(), "alice")
	if r := alice.upload(avatarRoute, "avatar", "a.png", "image/png", []byte(bannerPNG(t, 64, 64)), nil); r.status != http.StatusOK {
		t.Fatalf("avatar: %d", r.status)
	}
	if r := alice.delete(avatarRoute); r.status != http.StatusOK || strings.Contains(string(r.body), "avatar_url") {
		t.Fatalf("remove: %d %s", r.status, r.body)
	}
	if a.store.Len() != 0 || profileOf(t, alice, alice)["avatar_url"] != nil {
		t.Fatalf("the file and the reference are gone: %d objects", a.store.Len())
	}
	if r := alice.delete(avatarRoute); r.status != http.StatusOK {
		t.Fatalf("remove again: %d", r.status)
	}
	if r := a.anon().delete(avatarRoute); r.status != http.StatusUnauthorized {
		t.Fatalf("without a session: %d", r.status)
	}
}
