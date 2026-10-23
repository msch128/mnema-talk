package imgproc

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/png"

	xdraw "golang.org/x/image/draw"
)

// Frame is one complete picture of an animation, drawn onto the full canvas.
type Frame struct {
	Img *image.NRGBA
	Pal color.Palette
}

// EncodePNG writes img as a PNG with the best compression.
func EncodePNG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestCompression}
	if err := enc.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("encode png: %w", err)
	}
	return buf.Bytes(), nil
}

// ComposeGIF redraws every frame onto the full canvas (honouring disposal)
// and hands the canvas to shape, which crops or shrinks it.
func ComposeGIF(g *gif.GIF, cfg image.Config, shape func(image.Image) *image.NRGBA) []Frame {
	canvas := image.NewRGBA(image.Rect(0, 0, cfg.Width, cfg.Height))
	out := make([]Frame, 0, len(g.Image))
	var saved *image.RGBA // one reusable buffer for DisposalPrevious
	for i, frame := range g.Image {
		disposal := byte(0)
		if i < len(g.Disposal) {
			disposal = g.Disposal[i]
		}
		if disposal == gif.DisposalPrevious {
			if saved == nil {
				saved = image.NewRGBA(canvas.Rect)
			}
			copy(saved.Pix, canvas.Pix)
		}
		draw.Draw(canvas, frame.Bounds(), frame, frame.Bounds().Min, draw.Over)
		out = append(out, Frame{Img: shape(canvas), Pal: frame.Palette})
		switch disposal {
		case gif.DisposalBackground:
			draw.Draw(canvas, frame.Bounds(), image.Transparent, image.Point{}, draw.Src)
		case gif.DisposalPrevious:
			copy(canvas.Pix, saved.Pix)
		}
	}
	return out
}

// withTransparency makes sure the palette can draw a see-through pixel.
func withTransparency(p color.Palette) color.Palette {
	for _, c := range p {
		if _, _, _, a := c.RGBA(); a == 0 {
			return p
		}
	}
	out := append(color.Palette(nil), p...)
	if len(out) < 256 {
		return append(out, color.Transparent)
	}
	out[len(out)-1] = color.Transparent
	return out
}

// EncodeGIF writes frames as a new animation with the timing and loop count
// of the original g.
func EncodeGIF(g *gif.GIF, frames []Frame) ([]byte, error) {
	b := frames[0].Img.Bounds()
	res := &gif.GIF{LoopCount: g.LoopCount, Config: image.Config{Width: b.Dx(), Height: b.Dy()}}
	for i, f := range frames {
		p := image.NewPaletted(b, withTransparency(f.Pal))
		xdraw.FloydSteinberg.Draw(p, b, f.Img, b.Min)
		res.Image = append(res.Image, p)
		res.Delay = append(res.Delay, g.Delay[i])
		// Every frame is a complete picture: clear before the next one.
		res.Disposal = append(res.Disposal, gif.DisposalBackground)
	}
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, res); err != nil {
		return nil, fmt.Errorf("encode gif: %w", err)
	}
	return buf.Bytes(), nil
}
