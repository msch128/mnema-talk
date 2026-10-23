package emoji

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/gif"
	"image/png"
	"io"

	"github.com/msch128/mnema-talk/internal/imgproc"
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
	// maxAnimationPixels bounds canvas area times frames, which is what the
	// decoder and the redraw hold in memory; checked before any frame is decoded.
	maxAnimationPixels = 16 << 20
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
	encoded, err := imgproc.EncodePNG(scaled(src))
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
	n := imgproc.GIFFrames(data, maxFrames)
	if n < 1 || n > maxFrames {
		return nil, reject("an animation may have 1 to %d frames", maxFrames)
	}
	if cfg.Width*cfg.Height*n > maxAnimationPixels {
		return nil, reject("the animation is too large: canvas size times frames may not exceed %d million pixels", maxAnimationPixels>>20)
	}
	g, err := gif.DecodeAll(bytes.NewReader(data))
	if err != nil || len(g.Image) == 0 {
		return nil, reject("the image cannot be read")
	}
	frames := imgproc.ComposeGIF(g, cfg, func(src image.Image) *image.NRGBA { return scaled(src) })
	if len(frames) == 1 {
		encoded, err := imgproc.EncodePNG(frames[0].Img)
		if err != nil {
			return nil, err
		}
		return &Image{Data: encoded, ContentType: TypePNG}, nil
	}
	return encodeAnimation(g, frames)
}

func encodeAnimation(g *gif.GIF, frames []imgproc.Frame) (*Image, error) {
	still, err := imgproc.EncodePNG(frames[0].Img)
	if err != nil {
		return nil, err
	}
	data, err := imgproc.EncodeGIF(g, frames)
	if err != nil {
		return nil, err
	}
	return &Image{Data: data, ContentType: TypeGIF, Animated: true, Still: still}, nil
}
