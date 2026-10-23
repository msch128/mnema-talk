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

// Limits of a profile banner. The uploaded file is never stored: it is decoded,
// cropped to the banner's shape and drawn again, so metadata (EXIF, XMP, ICC),
// trailing bytes and anything hidden in the container (polyglots) are gone;
// only pixels survive.
const (
	// BannerMaxWidth and BannerMaxHeight are the largest stored banner (3:1).
	BannerMaxWidth  = 1500
	BannerMaxHeight = 500
	// BannerMinWidth and BannerMinHeight are the smallest source accepted once
	// it is cropped to 3:1 (smaller pictures would be blurry when stretched).
	BannerMinWidth  = 300
	BannerMinHeight = 100
	// bannerMaxEdge and bannerMaxPixels bound the source canvas, checked from
	// the header before anything is decoded.
	bannerMaxEdge   = 8000
	bannerMaxPixels = 16 << 20
	// bannerMaxFrames bounds an animation; counted before the frames are decoded.
	bannerMaxFrames = 100
	// BannerMaxAnimationPixels bounds canvas area times frames, which is what
	// the decoder and the redraw hold in memory. It is the same budget as the
	// animated custom emoji have.
	BannerMaxAnimationPixels = 16 << 20
	bannerJPEGQuality        = 85
)

// ErrImage is a rejected picture; its message is meant for the uploader.
var ErrImage = errors.New("invalid image")

func reject(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrImage, fmt.Sprintf(format, args...))
}

// Banner is a validated, re-encoded profile banner.
type Banner struct {
	Data []byte
	// ContentType is image/jpeg (JPEG sources), image/png (PNG, WebP and
	// single-frame GIF sources) or image/gif (animations).
	ContentType string
	Animated    bool
}

// ProcessBanner validates an uploaded PNG, JPEG, GIF or WebP and returns it
// cropped to the centre 3:1 and shrunk to at most BannerMaxWidth x
// BannerMaxHeight (never enlarged). Animated WebP is rejected; an animated GIF
// stays animated within BannerMaxAnimationPixels.
func ProcessBanner(data []byte) (*Banner, error) {
	switch {
	case bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")):
		return bannerStatic(data, png.DecodeConfig, png.Decode, "image/png")
	case bytes.HasPrefix(data, []byte("\xff\xd8\xff")):
		return bannerStatic(data, jpeg.DecodeConfig, jpeg.Decode, "image/jpeg")
	case bytes.HasPrefix(data, []byte("GIF87a")), bytes.HasPrefix(data, []byte("GIF89a")):
		return bannerGIF(data)
	case len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		if len(data) >= 21 && string(data[12:16]) == "VP8X" && data[20]&0x02 != 0 {
			return nil, reject("animated WebP is not supported, use a GIF")
		}
		return bannerStatic(data, webp.DecodeConfig, webp.Decode, "image/png")
	default:
		return nil, reject("only PNG, JPEG, GIF and WebP images are accepted")
	}
}

func checkBannerCanvas(cfg image.Config) error {
	if cfg.Width < BannerMinWidth || cfg.Height < BannerMinHeight {
		return reject("the image must be at least %dx%d pixels", BannerMinWidth, BannerMinHeight)
	}
	if cfg.Width > bannerMaxEdge || cfg.Height > bannerMaxEdge || cfg.Width*cfg.Height > bannerMaxPixels {
		return reject("the image is too large: at most %d pixels on an edge and %d million in total", bannerMaxEdge, bannerMaxPixels>>20)
	}
	return nil
}

// bannerRect is the centred 3:1 area of b, the largest one that fits.
func bannerRect(b image.Rectangle) image.Rectangle {
	w, h := b.Dx(), b.Dy()
	cw, ch := w, w/3
	if ch > h {
		cw, ch = h*3, h
	}
	x0, y0 := b.Min.X+(w-cw)/2, b.Min.Y+(h-ch)/2
	return image.Rect(x0, y0, x0+cw, y0+ch)
}

// coverBanner crops src to 3:1 and shrinks the crop to the banner maximum.
func coverBanner(src image.Image) *image.NRGBA {
	area := bannerRect(src.Bounds())
	w, h := area.Dx(), area.Dy()
	if w > BannerMaxWidth {
		w, h = BannerMaxWidth, BannerMaxHeight
	}
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	if w == area.Dx() {
		draw.Draw(dst, dst.Bounds(), src, area.Min, draw.Src)
	} else {
		xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, area, xdraw.Src, nil)
	}
	return dst
}

func bannerStatic(data []byte, config func(io.Reader) (image.Config, error), decode func(io.Reader) (image.Image, error), contentType string) (*Banner, error) {
	cfg, err := config(bytes.NewReader(data))
	if err != nil {
		return nil, reject("the image cannot be read")
	}
	if err := checkBannerCanvas(cfg); err != nil {
		return nil, err
	}
	src, err := decode(bytes.NewReader(data))
	if err != nil {
		return nil, reject("the image cannot be read")
	}
	return encodeBanner(coverBanner(src), contentType)
}

func encodeBanner(img *image.NRGBA, contentType string) (*Banner, error) {
	if contentType == "image/png" {
		out, err := EncodePNG(img)
		return &Banner{Data: out, ContentType: contentType}, err
	}
	// JPEG has no alpha: sources of this type are opaque, so nothing is lost.
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: bannerJPEGQuality}); err != nil {
		return nil, fmt.Errorf("encode jpeg: %w", err)
	}
	return &Banner{Data: buf.Bytes(), ContentType: contentType}, nil
}

// bannerGIF redraws every frame onto the full canvas, crops and shrinks it and
// writes a new GIF. A single-frame GIF becomes a PNG.
func bannerGIF(data []byte) (*Banner, error) {
	cfg, err := gif.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, reject("the image cannot be read")
	}
	if err := checkBannerCanvas(cfg); err != nil {
		return nil, err
	}
	n := GIFFrames(data, bannerMaxFrames)
	if n < 1 || n > bannerMaxFrames {
		return nil, reject("an animation may have 1 to %d frames", bannerMaxFrames)
	}
	if cfg.Width*cfg.Height*n > BannerMaxAnimationPixels {
		return nil, reject("the animation is too large: canvas size times frames may not exceed %d million pixels", BannerMaxAnimationPixels>>20)
	}
	g, err := gif.DecodeAll(bytes.NewReader(data))
	if err != nil || len(g.Image) == 0 {
		return nil, reject("the image cannot be read")
	}
	frames := ComposeGIF(g, cfg, coverBanner)
	if len(frames) == 1 {
		return encodeBanner(frames[0].Img, "image/png")
	}
	out, err := EncodeGIF(g, frames)
	if err != nil {
		return nil, err
	}
	return &Banner{Data: out, ContentType: "image/gif", Animated: true}, nil
}
