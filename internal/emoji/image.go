package emoji

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/png"
	"io"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/webp"
)

// Limits of an emoji upload. The uploaded file is never stored: it is decoded
// and drawn again, so metadata, trailing bytes and anything hidden in the
// container (polyglots) are gone; only pixels survive.
const (
	// MaxFileBytes is the size of the upload and of the stored result.
	MaxFileBytes = 256 << 10
	// MaxEdge is the longest edge of the stored image; larger ones are shrunk.
	MaxEdge = 128
	// maxSourceEdge bounds the canvas before anything is decoded, so a small
	// file cannot ask for gigabytes of pixels.
	maxSourceEdge = 1024
	// maxFrames bounds an animation; counted before the frames are decoded.
	maxFrames = 100
)

// Content types the stored result can have.
const (
	TypePNG = "image/png"
	TypeGIF = "image/gif"
)

// ErrImage is a rejected upload; its message is meant for the uploader.
var ErrImage = errors.New("invalid emoji image")

func reject(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrImage, fmt.Sprintf(format, args...))
}

// Image is a validated, re-encoded emoji.
type Image struct {
	Data        []byte
	ContentType string
	Animated    bool
	// Still is the first frame as PNG (animated emoji only), shown to members
	// who prefer reduced motion.
	Still []byte
	// Hash is the hex SHA-256 of Data; it versions the served URL.
	Hash string
}

// Process validates an uploaded PNG, GIF or WebP and returns it re-encoded: a
// PNG (static) or a GIF (animated), at most MaxEdge pixels on each edge. SVG,
// JPEG, animated WebP and every other type are rejected.
func Process(data []byte) (*Image, error) {
	if len(data) == 0 || len(data) > MaxFileBytes {
		return nil, reject("the file must be between 1 byte and %d KiB", MaxFileBytes>>10)
	}
	out, err := redraw(data)
	if err != nil {
		return nil, err
	}
	if len(out.Data) > MaxFileBytes {
		return nil, reject("the image is larger than %d KiB after processing", MaxFileBytes>>10)
	}
	sum := sha256.Sum256(out.Data)
	out.Hash = hex.EncodeToString(sum[:])
	return out, nil
}

func redraw(data []byte) (*Image, error) {
	switch {
	case bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")):
		return processStatic(data, png.DecodeConfig, png.Decode)
	case bytes.HasPrefix(data, []byte("GIF87a")), bytes.HasPrefix(data, []byte("GIF89a")):
		return processGIF(data)
	case isWebP(data):
		if isAnimatedWebP(data) {
			return nil, reject("animated WebP is not supported, use a GIF")
		}
		return processStatic(data, webp.DecodeConfig, webp.Decode)
	default:
		return nil, reject("only PNG, GIF and WebP images are accepted")
	}
}

func isWebP(data []byte) bool {
	return len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP"
}

// isAnimatedWebP reads the animation flag of the extended header (VP8X).
func isAnimatedWebP(data []byte) bool {
	return len(data) >= 21 && string(data[12:16]) == "VP8X" && data[20]&0x02 != 0
}

func checkCanvas(cfg image.Config) error {
	if cfg.Width < 1 || cfg.Height < 1 || cfg.Width > maxSourceEdge || cfg.Height > maxSourceEdge {
		return reject("the image must be between 1 and %d pixels on each edge", maxSourceEdge)
	}
	return nil
}

// fit returns the size that fits MaxEdge, keeping the aspect ratio and never
// enlarging.
func fit(w, h int) (int, int) {
	if w <= MaxEdge && h <= MaxEdge {
		return w, h
	}
	if w >= h {
		return MaxEdge, max(1, h*MaxEdge/w)
	}
	return max(1, w*MaxEdge/h), MaxEdge
}

func scaled(src image.Image) *image.NRGBA {
	b := src.Bounds()
	w, h := fit(b.Dx(), b.Dy())
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	if w == b.Dx() && h == b.Dy() {
		draw.Draw(dst, dst.Bounds(), src, b.Min, draw.Src)
	} else {
		xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, b, xdraw.Src, nil)
	}
	return dst
}

func encodePNG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestCompression}
	if err := enc.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("encode emoji: %w", err)
	}
	return buf.Bytes(), nil
}

func processStatic(data []byte, config func(io.Reader) (image.Config, error), decode func(io.Reader) (image.Image, error)) (*Image, error) {
	cfg, err := config(bytes.NewReader(data))
	if err != nil {
		return nil, reject("the image cannot be read")
	}
	if err := checkCanvas(cfg); err != nil {
		return nil, err
	}
	src, err := decode(bytes.NewReader(data))
	if err != nil {
		return nil, reject("the image cannot be read")
	}
	encoded, err := encodePNG(scaled(src))
	if err != nil {
		return nil, err
	}
	return &Image{Data: encoded, ContentType: TypePNG}, nil
}

// processGIF redraws every frame onto the full canvas (honouring disposal),
// shrinks it and writes a new GIF. A single-frame GIF becomes a PNG.
func processGIF(data []byte) (*Image, error) {
	cfg, err := gif.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, reject("the image cannot be read")
	}
	if err := checkCanvas(cfg); err != nil {
		return nil, err
	}
	if n := countGIFFrames(data); n < 1 || n > maxFrames {
		return nil, reject("an animation may have 1 to %d frames", maxFrames)
	}
	g, err := gif.DecodeAll(bytes.NewReader(data))
	if err != nil || len(g.Image) == 0 {
		return nil, reject("the image cannot be read")
	}
	frames := composeFrames(g, cfg)
	if len(frames) == 1 {
		encoded, err := encodePNG(frames[0].img)
		if err != nil {
			return nil, err
		}
		return &Image{Data: encoded, ContentType: TypePNG}, nil
	}
	return encodeAnimation(g, frames)
}

type composed struct {
	img *image.NRGBA
	pal color.Palette
}

func composeFrames(g *gif.GIF, cfg image.Config) []composed {
	canvas := image.NewRGBA(image.Rect(0, 0, cfg.Width, cfg.Height))
	out := make([]composed, 0, len(g.Image))
	var saved *image.RGBA
	for i, frame := range g.Image {
		disposal := byte(0)
		if i < len(g.Disposal) {
			disposal = g.Disposal[i]
		}
		if disposal == gif.DisposalPrevious {
			saved = cloneRGBA(canvas)
		}
		draw.Draw(canvas, frame.Bounds(), frame, frame.Bounds().Min, draw.Over)
		out = append(out, composed{img: scaled(canvas), pal: frame.Palette})
		switch disposal {
		case gif.DisposalBackground:
			draw.Draw(canvas, frame.Bounds(), image.Transparent, image.Point{}, draw.Src)
		case gif.DisposalPrevious:
			copy(canvas.Pix, saved.Pix)
		}
	}
	return out
}

func cloneRGBA(src *image.RGBA) *image.RGBA {
	dst := image.NewRGBA(src.Rect)
	copy(dst.Pix, src.Pix)
	return dst
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

func encodeAnimation(g *gif.GIF, frames []composed) (*Image, error) {
	b := frames[0].img.Bounds()
	still, err := encodePNG(frames[0].img)
	if err != nil {
		return nil, err
	}
	res := &gif.GIF{LoopCount: g.LoopCount, Config: image.Config{Width: b.Dx(), Height: b.Dy()}}
	for i, f := range frames {
		p := image.NewPaletted(b, withTransparency(f.pal))
		xdraw.FloydSteinberg.Draw(p, b, f.img, b.Min)
		res.Image = append(res.Image, p)
		res.Delay = append(res.Delay, g.Delay[i])
		// Every frame is a complete picture: clear before the next one.
		res.Disposal = append(res.Disposal, gif.DisposalBackground)
	}
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, res); err != nil {
		return nil, fmt.Errorf("encode emoji: %w", err)
	}
	return &Image{Data: buf.Bytes(), ContentType: TypeGIF, Animated: true, Still: still}, nil
}
