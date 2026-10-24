package imgproc

import (
	"image"
	"testing"
)

func TestAvatarIsCroppedToASquareAndShrunk(t *testing.T) {
	cases := []struct {
		name string
		w, h int
		want image.Point
	}{
		{"a wide photo keeps its centre square", 1200, 600, image.Pt(512, 512)},
		{"a tall photo keeps its centre square", 600, 1200, image.Pt(512, 512)},
		{"a small square is not enlarged", 64, 64, image.Pt(64, 64)},
		{"a small wide picture", 200, 80, image.Pt(80, 80)},
		{"a single pixel", 1, 1, image.Pt(1, 1)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := ProcessAvatar(pngOf(t, c.w, c.h))
			if err != nil {
				t.Fatal(err)
			}
			if got := sizeOf(t, out.Data); got != c.want || out.ContentType != "image/png" {
				t.Fatalf("got %v %s, want %v", got, out.ContentType, c.want)
			}
		})
	}
}

func TestAvatarAnimationRules(t *testing.T) {
	out, err := ProcessAvatar(gifOf(t, 300, 200, 3))
	if err != nil || !out.Animated || sizeOf(t, out.Data) != image.Pt(200, 200) {
		t.Fatalf("%v %v", out, err)
	}
	if out, err := ProcessAvatar(gifOf(t, 100, 100, 1)); err != nil || out.Animated || out.ContentType != "image/png" {
		t.Fatalf("one frame: %v %v", out, err)
	}
	if _, err := ProcessAvatar(gifOf(t, 600, 600, 50)); err == nil {
		t.Fatal("over the pixel budget")
	}
	if _, err := ProcessAvatar([]byte("RIFF\x00\x00\x00\x00WEBPVP8X\x0a\x00\x00\x00\x02" + string(make([]byte, 20)))); err == nil {
		t.Fatal("animated WebP")
	}
}
