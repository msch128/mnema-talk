package imgproc

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"runtime"
	"testing"
	"time"
)

// Lane 8, "limits before and after the work: image pixels against bytes". Each
// entry point gets a file of a few hundred bytes whose header claims a huge
// canvas. The limit must hold before the work: the call is refused, allocates
// a bounded amount and returns quickly. A decode of the claimed canvas would
// need gigabytes. Every case counts as run only if it was refused.

func onePixel() image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.NRGBA{R: 200, A: 255})
	return img
}

// hugePNG is a valid 1x1 PNG whose IHDR claims w x h (CRC fixed).
func hugePNG(t *testing.T, w, h uint32) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, onePixel()); err != nil {
		t.Fatal(err)
	}
	data := b.Bytes()
	// signature (8), length (4), "IHDR" (4), width, height, ...
	binary.BigEndian.PutUint32(data[16:], w)
	binary.BigEndian.PutUint32(data[20:], h)
	binary.BigEndian.PutUint32(data[29:], crc32.ChecksumIEEE(data[12:29]))
	return data
}

// hugeGIF is a 1x1 GIF whose logical screen claims w x h.
func hugeGIF(t *testing.T, w, h uint16) []byte {
	t.Helper()
	var b bytes.Buffer
	pal := image.NewPaletted(image.Rect(0, 0, 1, 1), color.Palette{color.Black, color.White})
	if err := gif.Encode(&b, pal, nil); err != nil {
		t.Fatal(err)
	}
	data := b.Bytes()
	binary.LittleEndian.PutUint16(data[6:], w)
	binary.LittleEndian.PutUint16(data[8:], h)
	return data
}

// hugeJPEG is a 1x1 JPEG whose SOF0 claims w x h.
func hugeJPEG(t *testing.T, w, h uint16) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, onePixel(), nil); err != nil {
		t.Fatal(err)
	}
	data := b.Bytes()
	i := bytes.Index(data, []byte{0xFF, 0xC0})
	if i < 0 {
		t.Fatal("no SOF0")
	}
	// marker (2), length (2), precision (1), height (2), width (2)
	binary.BigEndian.PutUint16(data[i+5:], h)
	binary.BigEndian.PutUint16(data[i+7:], w)
	return data
}

// hugeWebP is a VP8X header that claims a w x h canvas, and nothing else.
func hugeWebP(w, h uint32) []byte {
	chunk := make([]byte, 10)
	chunk[0] = 0x00 // flags: still
	put24 := func(b []byte, v uint32) { b[0], b[1], b[2] = byte(v), byte(v>>8), byte(v>>16) }
	put24(chunk[4:], w-1)
	put24(chunk[7:], h-1)
	body := append([]byte("WEBPVP8X"), 0, 0, 0, 0)
	binary.LittleEndian.PutUint32(body[8:], uint32(len(chunk)))
	body = append(body, chunk...)
	out := append([]byte("RIFF"), 0, 0, 0, 0)
	binary.LittleEndian.PutUint32(out[4:], uint32(len(body)))
	return append(out, body...)
}

func TestHugeCanvasesAreRefusedBeforeTheyAreDecoded(t *testing.T) {
	files := map[string][]byte{
		"png 65535x65535":  hugePNG(t, 65535, 65535),
		"png 1x1G":         hugePNG(t, 1, 1<<30),
		"gif 65535x65535":  hugeGIF(t, 65535, 65535),
		"jpeg 65535x65535": hugeJPEG(t, 65535, 65535),
		// x/image/webp itself refuses more than 2^31-1 pixels.
		"webp 40000x40000": hugeWebP(40000, 40000),
	}
	entries := map[string]func([]byte) (any, error){
		"banner":        func(d []byte) (any, error) { return ProcessBanner(d) },
		"avatar":        func(d []byte) (any, error) { return ProcessAvatar(d) },
		"legacy avatar": func(d []byte) (any, error) { return ProcessLegacyAvatar(d) },
		"inline":        func(d []byte) (any, error) { return ProcessInline(d) },
		"icon":          func(d []byte) (any, error) { return ProcessIcon(d) },
	}
	const allocBound = 8 << 20
	// The perturbation happened: each header reads as the huge canvas it
	// claims (a broken file would be refused for another reason).
	for fname, data := range files {
		cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil || int64(cfg.Width)*int64(cfg.Height) <= LegacyMaxPixels {
			t.Fatalf("%s: header reads as %dx%d (%v), not a huge canvas", fname, cfg.Width, cfg.Height, err)
		}
	}
	refused := 0
	for fname, data := range files {
		for ename, entry := range entries {
			var before, after runtime.MemStats
			runtime.GC()
			runtime.ReadMemStats(&before)
			start := time.Now()
			_, err := entry(data)
			took := time.Since(start)
			runtime.ReadMemStats(&after)
			alloc := after.TotalAlloc - before.TotalAlloc
			if err == nil {
				t.Errorf("%s via %s: accepted", fname, ename)
				continue
			}
			if alloc > allocBound || took > time.Second {
				t.Errorf("%s via %s: refused only after %d bytes allocated and %v (%v)", fname, ename, alloc, took, err)
				continue
			}
			refused++
		}
	}
	if want := len(files) * len(entries); refused != want {
		t.Fatalf("%d of %d huge canvases refused within the bounds", refused, want)
	}
	t.Logf("%d huge canvases (%d files x %d entry points) refused, each under %d bytes allocated", refused, len(files), len(entries), allocBound)
}
