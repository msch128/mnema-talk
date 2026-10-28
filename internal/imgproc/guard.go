package imgproc

import (
	"bytes"
	"image"
	"image/gif"
)

// DecodeGIF is the guarded first half of every GIF upload: the canvas is
// checked by canvasOK and the frame count and memory budget by the shared
// limits before any frame is decoded. reject builds the caller's refusal
// (its own error type and wording).
func DecodeGIF(data []byte, canvasOK func(image.Config) error, reject func(format string, args ...any) error) (*gif.GIF, image.Config, error) {
	cfg, err := gif.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, cfg, reject("the image cannot be read")
	}
	if err := canvasOK(cfg); err != nil {
		return nil, cfg, err
	}
	n := GIFFrames(data, MaxFrames)
	if n < 1 || n > MaxFrames {
		return nil, cfg, reject("an animation may have 1 to %d frames", MaxFrames)
	}
	if cfg.Width*cfg.Height*n > MaxAnimationPixels {
		return nil, cfg, reject("the animation is too large: canvas size times frames may not exceed %d million pixels", MaxAnimationPixels>>20)
	}
	g, err := gif.DecodeAll(bytes.NewReader(data))
	if err != nil || len(g.Image) == 0 {
		return nil, cfg, reject("the image cannot be read")
	}
	return g, cfg, nil
}

// IsWebP reports whether data is a RIFF/WEBP container.
func IsWebP(data []byte) bool {
	return len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP"
}

// IsAnimatedWebP reads the animation flag of the extended header (VP8X).
func IsAnimatedWebP(data []byte) bool {
	return len(data) >= 21 && string(data[12:16]) == "VP8X" && data[20]&0x02 != 0
}
