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

func TestEncodedFramesKeepTheirComposedColours(t *testing.T) {
	// Colours that no source palette held: the output must still show them.
	img := image.NewNRGBA(image.Rect(0, 0, 6, 2))
	want := []color.NRGBA{{200, 30, 40, 255}, {20, 180, 90, 255}, {10, 20, 230, 255}}
	for x := range 6 {
		for y := range 2 {
			img.SetNRGBA(x, y, want[x/2])
		}
	}
	img.SetNRGBA(5, 1, color.NRGBA{}) // see-through
	data, err := EncodeGIF(&gif.GIF{Delay: []int{3, 3}}, []Frame{{Img: img}, {Img: img}})
	if err != nil {
		t.Fatal(err)
	}
	out, err := gif.DecodeAll(bytes.NewReader(data))
	if err != nil || len(out.Image) != 2 {
		t.Fatalf("%v %v", out, err)
	}
	for i, f := range out.Image {
		for x := range 5 {
			r, g, b, _ := f.At(x, 0).RGBA()
			w := want[x/2]
			if abs(int(r>>8)-int(w.R)) > 8 || abs(int(g>>8)-int(w.G)) > 8 || abs(int(b>>8)-int(w.B)) > 8 {
				t.Fatalf("frame %d pixel %d = %d,%d,%d, want %v", i, x, r>>8, g>>8, b>>8, w)
			}
		}
		if _, _, _, a := f.At(5, 1).RGBA(); a != 0 {
			t.Errorf("frame %d: the see-through pixel became opaque", i)
		}
	}
}

func TestAdaptivePaletteStaysWithinOneGIFPalette(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	for x := range 64 {
		for y := range 64 {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 4), G: uint8(y * 4), B: uint8((x + y) * 2), A: 255})
		}
	}
	pal := adaptivePalette(img)
	if len(pal) < 200 || len(pal) > 256 {
		t.Fatalf("palette of %d colours", len(pal))
	}
	if _, _, _, a := pal[len(pal)-1].RGBA(); a != 0 {
		t.Fatal("the last entry must be see-through")
	}
	if _, err := EncodeGIF(&gif.GIF{Delay: []int{1}}, []Frame{{Img: img}}); err != nil {
		t.Fatal(err)
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
