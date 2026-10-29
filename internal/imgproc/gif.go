package imgproc

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"image/gif"
	"image/png"

	xdraw "golang.org/x/image/draw"
)

// Frame is one complete picture of an animation, drawn onto the full canvas.
type Frame struct {
	Img *image.NRGBA
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
		out = append(out, Frame{Img: shape(canvas)})
		switch disposal {
		case gif.DisposalBackground:
			draw.Draw(canvas, frame.Bounds(), image.Transparent, image.Point{}, draw.Src)
		case gif.DisposalPrevious:
			copy(canvas.Pix, saved.Pix)
		}
	}
	return out
}

// hardAlpha returns a copy of img whose pixels are fully opaque (alpha >= 128)
// or fully transparent with a zeroed colour.
func hardAlpha(img *image.NRGBA) *image.NRGBA {
	out := image.NewNRGBA(img.Rect)
	for y := img.Rect.Min.Y; y < img.Rect.Max.Y; y++ {
		src := img.Pix[(y-img.Rect.Min.Y)*img.Stride:]
		dst := out.Pix[(y-img.Rect.Min.Y)*out.Stride:]
		for i := 0; i < img.Rect.Dx()*4; i += 4 {
			if src[i+3] >= 128 {
				dst[i], dst[i+1], dst[i+2], dst[i+3] = src[i], src[i+1], src[i+2], 255
			}
		}
	}
	return out
}

// EncodeGIF writes frames as a new animation with the timing and loop count
// of the original g. Every frame gets a palette of its own colours: the
// composed canvas mixes pixels from earlier frames and scaling blends new
// colours, so the source frame's palette would not fit.
func EncodeGIF(g *gif.GIF, frames []Frame) ([]byte, error) {
	b := frames[0].Img.Bounds()
	res := &gif.GIF{LoopCount: g.LoopCount, Config: image.Config{Width: b.Dx(), Height: b.Dy()}}
	for i, f := range frames {
		// A GIF pixel is opaque or see-through. Deciding that before the palette
		// is built keeps the dithering, which compares premultiplied colours,
		// from snapping antialiased edges to dark entries.
		img := hardAlpha(f.Img)
		p := image.NewPaletted(b, adaptivePalette(img))
		xdraw.FloydSteinberg.Draw(p, b, img, b.Min)
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
