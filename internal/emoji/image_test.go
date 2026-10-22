package emoji

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"testing"
)

func pngOf(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.NRGBA{R: uint8(x), G: uint8(y), B: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func gifOf(t *testing.T, w, h, frames int) []byte {
	t.Helper()
	pal := color.Palette{color.Transparent, color.RGBA{R: 255, A: 255}, color.RGBA{B: 255, A: 255}}
	g := &gif.GIF{LoopCount: 0}
	for i := 0; i < frames; i++ {
		p := image.NewPaletted(image.Rect(0, 0, w, h), pal)
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				p.SetColorIndex(x, y, uint8(1+(x+i)%2))
			}
		}
		g.Image = append(g.Image, p)
		g.Delay = append(g.Delay, 5)
	}
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, g); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func bounds(t *testing.T, data []byte) image.Config {
	t.Helper()
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestLargePNGIsShrunkToTheEmojiSize(t *testing.T) {
	out, err := Process(pngOf(t, 400, 200))
	if err != nil {
		t.Fatal(err)
	}
	cfg := bounds(t, out.Data)
	if out.ContentType != TypePNG || out.Animated || cfg.Width != 128 || cfg.Height != 64 || out.Hash == "" {
		t.Fatalf("%+v %+v", out, cfg)
	}
}

func TestSmallPNGIsNotEnlarged(t *testing.T) {
	out, err := Process(pngOf(t, 32, 24))
	if err != nil {
		t.Fatal(err)
	}
	if cfg := bounds(t, out.Data); cfg.Width != 32 || cfg.Height != 24 {
		t.Fatalf("%+v", cfg)
	}
}

func TestBytesAfterThePNGAreNotKept(t *testing.T) {
	polyglot := append(pngOf(t, 16, 16), []byte("<script>alert(1)</script>PK\x03\x04hidden")...)
	out, err := Process(polyglot)
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"<script>", "PK\x03\x04", "hidden"} {
		if bytes.Contains(out.Data, []byte(marker)) {
			t.Fatalf("output still carries %q", marker)
		}
	}
}

func TestMetadataChunksAreNotKept(t *testing.T) {
	src := pngOf(t, 16, 16)
	iend := bytes.LastIndex(src, []byte("IEND")) - 4
	body := []byte("tEXtAuthor\x00bob")
	text := binary.BigEndian.AppendUint32(nil, uint32(len(body)-4))
	text = append(text, body...)
	text = binary.BigEndian.AppendUint32(text, crc32.ChecksumIEEE(body))
	withText := append(append(append([]byte{}, src[:iend]...), text...), src[iend:]...)
	out, err := Process(withText)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(out.Data, []byte("tEXt")) {
		t.Fatal("text chunk survived")
	}
}

func TestAnimatedGIFStaysAnimatedWithAStill(t *testing.T) {
	out, err := Process(gifOf(t, 200, 200, 4))
	if err != nil {
		t.Fatal(err)
	}
	g, err := gif.DecodeAll(bytes.NewReader(out.Data))
	if err != nil {
		t.Fatal(err)
	}
	if !out.Animated || out.ContentType != TypeGIF || len(g.Image) != 4 || g.Config.Width != 128 || len(out.Still) == 0 {
		t.Fatalf("animated=%v frames=%d %+v", out.Animated, len(g.Image), g.Config)
	}
	if cfg := bounds(t, out.Still); cfg.Width != 128 {
		t.Fatalf("still %+v", cfg)
	}
}

func TestSingleFrameGIFBecomesAPNG(t *testing.T) {
	out, err := Process(gifOf(t, 20, 20, 1))
	if err != nil {
		t.Fatal(err)
	}
	if out.Animated || out.ContentType != TypePNG || !bytes.HasPrefix(out.Data, []byte("\x89PNG")) {
		t.Fatalf("%+v", out.ContentType)
	}
}

func TestStaticWebPIsStoredAsPNG(t *testing.T) {
	// A 1x1 lossless WebP.
	webp, err := base64.StdEncoding.DecodeString("UklGRhoAAABXRUJQVlA4TA0AAAAvAAAAEAcQERGIiP4HAA==")
	if err != nil {
		t.Fatal(err)
	}
	out, err := Process(webp)
	if err != nil {
		t.Fatal(err)
	}
	if out.ContentType != TypePNG || !bytes.HasPrefix(out.Data, []byte("\x89PNG")) {
		t.Fatalf("%s", out.ContentType)
	}
}

func rejected(t *testing.T, name string, data []byte) {
	t.Helper()
	if _, err := Process(data); !errors.Is(err, ErrImage) {
		t.Errorf("%s: err = %v", name, err)
	}
}

func TestUnsupportedAndHostileFilesAreRejected(t *testing.T) {
	var jpg bytes.Buffer
	if err := jpeg.Encode(&jpg, image.NewRGBA(image.Rect(0, 0, 8, 8)), nil); err != nil {
		t.Fatal(err)
	}
	animatedWebP := []byte("RIFF\x1a\x00\x00\x00WEBPVP8X\x0a\x00\x00\x00\x12\x00\x00\x00\x00\x00\x00\x00\x00\x00")
	rejected(t, "svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`))
	rejected(t, "svg declared as png", append([]byte("<?xml version=\"1.0\"?>"), []byte("<svg/>")...))
	rejected(t, "html", []byte("<html><body>hi</body></html>"))
	rejected(t, "jpeg", jpg.Bytes())
	rejected(t, "animated webp", animatedWebP)
	rejected(t, "empty", nil)
	rejected(t, "truncated png", pngOf(t, 16, 16)[:40])
	rejected(t, "too large", bytes.Repeat([]byte{1}, MaxFileBytes+1))
	rejected(t, "huge canvas", pngOf(t, 1025, 4))
	rejected(t, "too many frames", gifOf(t, 4, 4, maxFrames+1))
	rejected(t, "gif header only", []byte("GIF89a"))
}
