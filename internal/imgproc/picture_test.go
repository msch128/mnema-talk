package imgproc

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"testing"
)

func solid(w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = 200, 40, 90, 255
	}
	return img
}

func pngOf(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, solid(w, h)); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func gifOf(t *testing.T, w, h, frames int) []byte {
	t.Helper()
	pal := color.Palette{color.Black, color.White}
	g := &gif.GIF{}
	for range frames {
		g.Image = append(g.Image, image.NewPaletted(image.Rect(0, 0, w, h), pal))
		g.Delay = append(g.Delay, 5)
	}
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, g); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func sizeOf(t *testing.T, data []byte) image.Point {
	t.Helper()
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return image.Pt(cfg.Width, cfg.Height)
}

func TestBannerIsCroppedToThreeToOneAndShrunk(t *testing.T) {
	cases := []struct {
		name string
		w, h int
		want image.Point
	}{
		{"wide picture is cropped at the sides", 3000, 600, image.Pt(1500, 500)},
		{"tall picture is cropped top and bottom", 900, 900, image.Pt(900, 300)},
		{"small 3:1 picture is not enlarged", 600, 200, image.Pt(600, 200)},
		{"a very tall one keeps its width", 450, 4000, image.Pt(450, 150)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := ProcessBanner(pngOf(t, c.w, c.h))
			if err != nil {
				t.Fatal(err)
			}
			if got := sizeOf(t, out.Data); got != c.want || out.ContentType != "image/png" || out.Animated {
				t.Fatalf("got %v %s animated=%v, want %v png", got, out.ContentType, out.Animated, c.want)
			}
		})
	}
}

func TestBannerJPEGStaysJPEG(t *testing.T) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, solid(1800, 900), nil); err != nil {
		t.Fatal(err)
	}
	out, err := ProcessBanner(buf.Bytes())
	if err != nil || out.ContentType != "image/jpeg" || sizeOf(t, out.Data) != image.Pt(1500, 500) {
		t.Fatalf("%v %v", out, err)
	}
}

func TestBannerAnimationStaysAnimatedWithinBudget(t *testing.T) {
	out, err := ProcessBanner(gifOf(t, 600, 300, 3))
	if err != nil {
		t.Fatal(err)
	}
	if !out.Animated || out.ContentType != "image/gif" || sizeOf(t, out.Data) != image.Pt(600, 200) {
		t.Fatalf("animated=%v %s", out.Animated, out.ContentType)
	}
	g, err := gif.DecodeAll(bytes.NewReader(out.Data))
	if err != nil || len(g.Image) != 3 {
		t.Fatalf("frames kept: %v %v", len(g.Image), err)
	}
}

func TestBannerSingleFrameGIFBecomesPNG(t *testing.T) {
	out, err := ProcessBanner(gifOf(t, 600, 200, 1))
	if err != nil || out.ContentType != "image/png" || out.Animated {
		t.Fatalf("%v %v", out, err)
	}
}

func TestBannerRejectsWhatItCannotTrust(t *testing.T) {
	wide := bytes.Repeat([]byte{0}, 20)
	cases := map[string][]byte{
		"empty":                  nil,
		"svg":                    []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`),
		"too small":              pngOf(t, 200, 80),
		"too many pixels":        pngOf(t, 5000, 4000),
		"animation over budget":  gifOf(t, 600, 600, 50),
		"too many frames":        gifOf(t, 300, 100, 101),
		"truncated png":          pngOf(t, 600, 200)[:60],
		"animated webp":          append([]byte("RIFF\x00\x00\x00\x00WEBPVP8X\x0a\x00\x00\x00\x02"), wide...),
		"broken gif":             []byte("GIF89a\x01\x00\x01\x00"),
		"png signature only":     []byte("\x89PNG\r\n\x1a\n"),
		"jpeg signature only":    []byte("\xff\xd8\xff"),
		"webp without a payload": []byte("RIFF\x04\x00\x00\x00WEBP"),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ProcessBanner(data); !errors.Is(err, ErrImage) {
				t.Fatalf("want ErrImage, got %v", err)
			}
		})
	}
}

func TestGIFFramesCountsWithoutDecoding(t *testing.T) {
	if n := GIFFrames(gifOf(t, 40, 40, 4), 100); n != 4 {
		t.Fatalf("frames: %d", n)
	}
	if n := GIFFrames(gifOf(t, 40, 40, 6), 3); n != 4 {
		t.Fatalf("stops just past the limit: %d", n)
	}
	if n := GIFFrames([]byte("GIF89a"), 10); n != 0 {
		t.Fatalf("short data: %d", n)
	}
}

func TestIconIsCroppedSquareAndDrawnAtTwoSizes(t *testing.T) {
	for _, c := range []struct {
		name string
		data []byte
	}{
		{"wide png", pngOf(t, 900, 300)},
		{"tall png", pngOf(t, 200, 700)},
		{"small png", pngOf(t, 128, 128)},
		{"single frame gif", gifOf(t, 300, 300, 1)},
	} {
		icon, err := ProcessIcon(c.data)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got := sizeOf(t, icon.Large); got != image.Pt(IconLargeEdge, IconLargeEdge) {
			t.Errorf("%s large: %v", c.name, got)
		}
		if got := sizeOf(t, icon.Small); got != image.Pt(IconSmallEdge, IconSmallEdge) {
			t.Errorf("%s small: %v", c.name, got)
		}
		if !bytes.HasPrefix(icon.Large, []byte("\x89PNG")) || !bytes.HasPrefix(icon.Small, []byte("\x89PNG")) {
			t.Errorf("%s: icons are PNGs", c.name)
		}
	}
}

func TestIconRejectsWhatItCannotBe(t *testing.T) {
	for name, data := range map[string][]byte{
		"too small":    pngOf(t, 100, 100),
		"too large":    pngOf(t, 9000, 200),
		"animated gif": gifOf(t, 300, 300, 3),
		"svg":          []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`),
		"text":         []byte("not an image"),
	} {
		if _, err := ProcessIcon(data); !errors.Is(err, ErrImage) {
			t.Errorf("%s: %v, want ErrImage", name, err)
		}
	}
}
