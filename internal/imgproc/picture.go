package imgproc

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/webp"
)

// Profile pictures (avatars and banners) are never stored as uploaded: the
// upload is decoded, cropped to the picture's shape and drawn again, so
// metadata (EXIF with GPS and camera model, XMP, ICC), trailing bytes and
// anything hidden in the container (polyglots) are gone; only pixels survive.
const (
	// maxEdge and maxPixels bound the source canvas, checked from the header
	// before anything is decoded.
	maxEdge   = 8000
	maxPixels = 16 << 20
	// maxFrames bounds an animation; counted before the frames are decoded.
	maxFrames = 100
	// MaxAnimationPixels bounds canvas area times frames, which is what the
	// decoder and the redraw hold in memory. It is the budget animated custom
	// emoji have as well.
	MaxAnimationPixels = 16 << 20
	jpegQuality        = 85
)

// ErrImage is a rejected picture; its message is meant for the uploader.
var ErrImage = errors.New("invalid image")

func reject(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrImage, fmt.Sprintf(format, args...))
}

// Picture is a validated, re-encoded profile picture.
type Picture struct {
	Data []byte
	// ContentType is image/jpeg (JPEG sources), image/png (PNG, WebP and
	// single-frame GIF sources) or image/gif (animations).
	ContentType string
	Animated    bool
}

// kind describes one sort of profile picture: the smallest accepted source and
// how a decoded picture is cropped and shrunk.
type kind struct {
	minW, minH int
	cover      func(image.Image) *image.NRGBA
}

// ProcessBanner returns an uploaded PNG, JPEG, GIF or WebP cropped to the
// centre 3:1 and shrunk to at most BannerMaxWidth x BannerMaxHeight (never
// enlarged). Animated WebP is rejected; an animated GIF stays animated within
// MaxAnimationPixels.
func ProcessBanner(data []byte) (*Picture, error) {
	return process(data, kind{minW: BannerMinWidth, minH: BannerMinHeight, cover: coverBanner})
}

// ProcessAvatar returns an uploaded PNG, JPEG, GIF or WebP cropped to the
// centre square and shrunk to at most AvatarMaxEdge pixels (never enlarged),
// with the same animation rules as ProcessBanner.
func ProcessAvatar(data []byte) (*Picture, error) {
	return process(data, kind{minW: 1, minH: 1, cover: coverAvatar})
}

func process(data []byte, k kind) (*Picture, error) {
	switch {
	case bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")):
		return still(data, k, png.DecodeConfig, png.Decode, "image/png")
	case bytes.HasPrefix(data, []byte("\xff\xd8\xff")):
		return still(data, k, jpeg.DecodeConfig, jpeg.Decode, "image/jpeg")
	case bytes.HasPrefix(data, []byte("GIF87a")), bytes.HasPrefix(data, []byte("GIF89a")):
		return animation(data, k)
	case len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		if len(data) >= 21 && string(data[12:16]) == "VP8X" && data[20]&0x02 != 0 {
			return nil, reject("animated WebP is not supported, use a GIF")
		}
		return still(data, k, webp.DecodeConfig, webp.Decode, "image/png")
	default:
		return nil, reject("only PNG, JPEG, GIF and WebP images are accepted")
	}
}

func checkCanvas(cfg image.Config, k kind) error {
	if cfg.Width < k.minW || cfg.Height < k.minH {
		return reject("the image must be at least %dx%d pixels", k.minW, k.minH)
	}
	if cfg.Width > maxEdge || cfg.Height > maxEdge || cfg.Width*cfg.Height > maxPixels {
		return reject("the image is too large: at most %d pixels on an edge and %d million in total", maxEdge, maxPixels>>20)
	}
	return nil
}

// centred is the largest area of b with the aspect ratio num:den, centred.
func centred(b image.Rectangle, num, den int) image.Rectangle {
	w, h := b.Dx(), b.Dy()
	cw, ch := w, w*den/num
	if ch > h {
		cw, ch = h*num/den, h
	}
	x0, y0 := b.Min.X+(w-cw)/2, b.Min.Y+(h-ch)/2
	return image.Rect(x0, y0, x0+cw, y0+ch)
}

// crop draws area of src into a new picture of size w x h, scaling when needed.
func crop(src image.Image, area image.Rectangle, w, h int) *image.NRGBA {
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	if w == area.Dx() && h == area.Dy() {
		draw.Draw(dst, dst.Bounds(), src, area.Min, draw.Src)
	} else {
		xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, area, xdraw.Src, nil)
	}
	return dst
}

func coverBanner(src image.Image) *image.NRGBA {
	area := centred(src.Bounds(), 3, 1)
	if area.Dx() > BannerMaxWidth {
		return crop(src, area, BannerMaxWidth, BannerMaxHeight)
	}
	return crop(src, area, area.Dx(), area.Dy())
}

func coverAvatar(src image.Image) *image.NRGBA {
	area := centred(src.Bounds(), 1, 1)
	return crop(src, area, min(area.Dx(), AvatarMaxEdge), min(area.Dy(), AvatarMaxEdge))
}

func still(data []byte, k kind, config func(io.Reader) (image.Config, error), decode func(io.Reader) (image.Image, error), contentType string) (*Picture, error) {
	cfg, err := config(bytes.NewReader(data))
	if err != nil {
		return nil, reject("the image cannot be read")
	}
	if err := checkCanvas(cfg, k); err != nil {
		return nil, err
	}
	src, err := decode(bytes.NewReader(data))
	if err != nil {
		return nil, reject("the image cannot be read")
	}
	return encode(k.cover(src), contentType)
}

func encode(img *image.NRGBA, contentType string) (*Picture, error) {
	if contentType == "image/png" {
		out, err := EncodePNG(img)
		return &Picture{Data: out, ContentType: contentType}, err
	}
	// JPEG has no alpha: sources of this type are opaque, so nothing is lost.
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: jpegQuality}); err != nil {
		return nil, fmt.Errorf("encode jpeg: %w", err)
	}
	return &Picture{Data: buf.Bytes(), ContentType: contentType}, nil
}

// animation redraws every frame onto the full canvas, crops and shrinks it and
// writes a new GIF. A single-frame GIF becomes a PNG.
func animation(data []byte, k kind) (*Picture, error) {
	cfg, err := gif.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, reject("the image cannot be read")
	}
	if err := checkCanvas(cfg, k); err != nil {
		return nil, err
	}
	n := GIFFrames(data, maxFrames)
	if n < 1 || n > maxFrames {
		return nil, reject("an animation may have 1 to %d frames", maxFrames)
	}
	if cfg.Width*cfg.Height*n > MaxAnimationPixels {
		return nil, reject("the animation is too large: canvas size times frames may not exceed %d million pixels", MaxAnimationPixels>>20)
	}
	g, err := gif.DecodeAll(bytes.NewReader(data))
	if err != nil || len(g.Image) == 0 {
		return nil, reject("the image cannot be read")
	}
	frames := ComposeGIF(g, cfg, k.cover)
	if len(frames) == 1 {
		return encode(frames[0].Img, "image/png")
	}
	out, err := EncodeGIF(g, frames)
	if err != nil {
		return nil, err
	}
	return &Picture{Data: out, ContentType: "image/gif", Animated: true}, nil
}
