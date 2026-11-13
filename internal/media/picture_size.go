package media

import (
	"bytes"
	"image"
	_ "image/gif"  // registers GIF for image.DecodeConfig
	_ "image/jpeg" // registers JPEG
	_ "image/png"  // registers PNG
	"strings"

	_ "golang.org/x/image/webp" // registers WebP
)

// pictureHeadBytes is how much of a stored file is kept to read a picture's
// size from: the header of a PNG, GIF or WebP, and of a JPEG whose metadata
// was removed, lies well inside it.
const pictureHeadBytes = 64 << 10

// maxPictureSide is the largest width or height recorded (the column's check).
const maxPictureSide = 65535

// pictureSize reads the width and height of a picture from the first bytes
// of the stored file, so a removed picture keeps its place in the chat. It
// answers nil for anything else and for a header it cannot read.
func pictureSize(mimeType string, head []byte) (width, height *int) {
	if !strings.HasPrefix(mimeType, "image/") {
		return nil, nil
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(head))
	if err != nil || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > maxPictureSide || cfg.Height > maxPictureSide {
		return nil, nil
	}
	return &cfg.Width, &cfg.Height
}
