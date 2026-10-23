package imgproc

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"testing"
)

func TestAnimationKeepsTimingAndLoopWhateverTheDisposal(t *testing.T) {
	pal := color.Palette{color.Black, color.White}
	g := &gif.GIF{LoopCount: 2}
	for i, disposal := range []byte{gif.DisposalNone, gif.DisposalBackground, gif.DisposalPrevious} {
		frame := image.NewPaletted(image.Rect(0, 0, 600, 200), pal)
		frame.SetColorIndex(i*10, 5, 1)
		g.Image = append(g.Image, frame)
		g.Delay = append(g.Delay, 7)
		g.Disposal = append(g.Disposal, disposal)
	}
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, g); err != nil {
		t.Fatal(err)
	}
	out, err := ProcessBanner(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	got, err := gif.DecodeAll(bytes.NewReader(out.Data))
	if err != nil || len(got.Image) != 3 || got.Delay[0] != 7 || got.LoopCount != 2 {
		t.Fatalf("frames=%d err=%v", len(got.Image), err)
	}
}

func TestEveryFrameCanBeSeeThrough(t *testing.T) {
	full := make(color.Palette, 256)
	for i := range full {
		full[i] = color.NRGBA{R: uint8(i), G: 1, B: 2, A: 255}
	}
	frames := make([]Frame, 2)
	for i, pal := range []color.Palette{full, {color.Black}} {
		frames[i] = Frame{Img: image.NewNRGBA(image.Rect(0, 0, 4, 4)), Pal: pal}
	}
	data, err := EncodeGIF(&gif.GIF{Delay: []int{3, 3}}, frames)
	if err != nil {
		t.Fatal(err)
	}
	if out, err := gif.DecodeAll(bytes.NewReader(data)); err != nil || len(out.Image) != 2 {
		t.Fatalf("%v %v", out, err)
	}
	if got := withTransparency(color.Palette{color.Transparent, color.White}); len(got) != 2 {
		t.Fatalf("a palette that is see-through already is kept: %d", len(got))
	}
}
