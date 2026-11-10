package imgproc

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"testing"
)

// fuzzMaxEdge bounds the canvas the fuzz targets decode, so a mutated header
// cannot make one run allocate gigabytes; the size limits themselves are the
// callers' and have their own tests.
const fuzzMaxEdge = 64

// afterEmptyApplicationHeader returns the animation data with a block put
// before its first frame that the specification and the decoder read
// differently: an application extension whose header is empty, followed by
// bytes that are one small image and a trailer when the empty header is taken
// for the end of the extension, and one data sub-block when it is not.
func afterEmptyApplicationHeader(data []byte) []byte {
	at := 13 + colorTableSize(data[10])
	decoy := append([]byte{0x2c, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x02, 31}, make([]byte, 31)...)
	out := append(bytes.Clone(data[:at]), 0x21, gifApplication, 0x00)
	out = append(out, decoy...)
	out = append(out, 0x00, 0x3b, 0x00)
	return append(out, data[at:]...)
}

// afterOddPlainText is the same construction with a plain text extension
// whose size byte is not the 12 the specification fixes.
func afterOddPlainText(data []byte) []byte {
	at := 13 + colorTableSize(data[10])
	out := append(bytes.Clone(data[:at]), 0x21, gifPlainText, 0x01, 0xaa, 0x00, 0x3b)
	out = append(out, make([]byte, 9)...)
	out = append(out, 0x00)
	return append(out, data[at:]...)
}

// gifSeeds are well-formed and broken animations as the unit tests build them.
func gifSeeds(f *testing.F) [][]byte {
	two := gifOf(f, 8, 4, 2)
	var mixed bytes.Buffer
	g := &gif.GIF{LoopCount: 2, Config: image.Config{Width: 8, Height: 5}}
	for i, disposal := range []byte{gif.DisposalNone, gif.DisposalBackground, gif.DisposalPrevious} {
		frame := image.NewPaletted(image.Rect(i, 0, 6+i, 5), color.Palette{color.Black, color.White})
		frame.SetColorIndex(i, 2, 1)
		g.Image = append(g.Image, frame)
		g.Delay = append(g.Delay, 7)
		g.Disposal = append(g.Disposal, disposal)
	}
	if err := gif.EncodeAll(&mixed, g); err != nil {
		f.Fatal(err)
	}
	return [][]byte{
		two, mixed.Bytes(), gifOf(f, 1, 1, 1), gifOf(f, 3, 3, MaxFrames+1),
		afterEmptyApplicationHeader(two), afterOddPlainText(two), afterEmptyApplicationHeader(gifOf(f, 3, 3, MaxFrames+1)),
		two[:len(two)-1], two[:len(two)/2], []byte("GIF89a\x01"), []byte("GIF89a"), nil,
	}
}

// FuzzGIFFrames checks the structure walk that counts the frames of an
// upload before it is decoded: it never panics, stops counting right after
// the limit, and for every file the decoder accepts it counts exactly the
// frames the decoder returns. The frame limit and the memory budget rest on
// that count, so no file may show the walk fewer frames than get decoded.
func FuzzGIFFrames(f *testing.F) {
	for _, seed := range gifSeeds(f) {
		f.Add(seed, MaxFrames)
		f.Add(seed, 1)
	}
	f.Fuzz(func(t *testing.T, data []byte, limit int) {
		n := GIFFrames(data, limit)
		if n < 0 || (limit >= 0 && n > limit+1) {
			t.Fatalf("%d frames counted with a limit of %d", n, limit)
		}
		cfg, err := gif.DecodeConfig(bytes.NewReader(data))
		if err != nil || cfg.Width > fuzzMaxEdge || cfg.Height > fuzzMaxEdge {
			return
		}
		g, err := gif.DecodeAll(bytes.NewReader(data))
		if err != nil {
			return
		}
		if all := GIFFrames(data, len(data)); all != len(g.Image) {
			t.Fatalf("%d frames counted, %d decoded", all, len(g.Image))
		}
	})
}

// FuzzDecodeGIF checks the guarded decode of a GIF upload: it never panics,
// an animation it lets through is within the frame limit and the memory
// budget, and the redraw that follows yields every frame.
func FuzzDecodeGIF(f *testing.F) {
	for _, seed := range gifSeeds(f) {
		f.Add(seed)
	}
	refused := errors.New("refused")
	canvasOK := func(cfg image.Config) error {
		if cfg.Width > fuzzMaxEdge || cfg.Height > fuzzMaxEdge {
			return refused
		}
		return nil
	}
	reject := func(format string, args ...any) error { return fmt.Errorf(format, args...) }
	f.Fuzz(func(t *testing.T, data []byte) {
		g, cfg, err := DecodeGIF(data, canvasOK, reject)
		if err != nil {
			if g != nil {
				t.Fatalf("an animation came back with the refusal %v", err)
			}
			return
		}
		if n := len(g.Image); n < 1 || n > MaxFrames || n != GIFFrames(data, MaxFrames) {
			t.Fatalf("%d frames decoded, %d counted before", n, GIFFrames(data, MaxFrames))
		}
		if cfg.Width*cfg.Height*len(g.Image) > MaxAnimationPixels {
			t.Fatalf("%dx%d with %d frames is over the budget", cfg.Width, cfg.Height, len(g.Image))
		}
		frames := ComposeGIF(g, cfg, func(canvas image.Image) *image.NRGBA {
			return image.NewNRGBA(canvas.Bounds())
		})
		if len(frames) != len(g.Image) {
			t.Fatalf("%d frames redrawn out of %d", len(frames), len(g.Image))
		}
	})
}
