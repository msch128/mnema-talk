package imgproc

import (
	"image"
)

// Icon is the redrawn community icon in the two sizes the server hands out.
// Both are PNGs, metadata free.
type Icon struct {
	Large []byte // IconLargeEdge square
	Small []byte // IconSmallEdge square
}

// ProcessIcon returns an uploaded PNG, JPEG, GIF or WebP cropped to the
// centre square and drawn again at IconLargeEdge and IconSmallEdge pixels, as
// PNG. Like every profile picture it is never kept as uploaded: only pixels
// survive. Animations are rejected, the icon is a still.
func ProcessIcon(data []byte) (*Icon, error) {
	var large *image.NRGBA
	cover := func(src image.Image) *image.NRGBA {
		large = crop(src, centred(src.Bounds(), 1, 1), IconLargeEdge, IconLargeEdge)
		return large
	}
	pic, err := process(data, kind{minW: IconMinEdge, minH: IconMinEdge, cover: cover})
	if err != nil {
		return nil, err
	}
	if pic.Animated || large == nil {
		return nil, reject("an animated image cannot be the icon, use a still picture")
	}
	big, err := EncodePNG(large)
	if err != nil {
		return nil, err
	}
	small, err := EncodePNG(crop(large, large.Bounds(), IconSmallEdge, IconSmallEdge))
	if err != nil {
		return nil, err
	}
	return &Icon{Large: big, Small: small}, nil
}
