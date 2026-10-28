package imgproc

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/draw"
)

// jpegOrientation returns the EXIF orientation (1 to 8) of a JPEG, or 1 when
// it has none or it cannot be read. Go's decoder ignores the tag and the
// redraw drops it, so without applying it phone photos would end up on their
// side.
func jpegOrientation(data []byte) int {
	pos := 2 // after SOI
	for pos+4 <= len(data) && data[pos] == 0xFF {
		marker := data[pos+1]
		if marker == 0xFF { // fill byte
			pos++
			continue
		}
		if marker == 0xDA || marker == 0xD9 || (marker >= 0xD0 && marker <= 0xD8) {
			return 1 // image data or end: no more headers
		}
		size := int(binary.BigEndian.Uint16(data[pos+2 : pos+4]))
		end := pos + 2 + size
		if size < 2 || end > len(data) {
			return 1
		}
		if marker == 0xE1 && bytes.HasPrefix(data[pos+4:end], []byte("Exif\x00\x00")) {
			return tiffOrientation(data[pos+10 : end])
		}
		pos = end
	}
	return 1
}

// tiffOrientation reads tag 0x0112 from the first IFD of an EXIF TIFF block.
func tiffOrientation(t []byte) int {
	if len(t) < 8 {
		return 1
	}
	var order binary.ByteOrder
	switch string(t[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 1
	}
	ifd := int(order.Uint32(t[4:8]))
	if ifd < 8 || ifd+2 > len(t) {
		return 1
	}
	n := int(order.Uint16(t[ifd : ifd+2]))
	for i := range n {
		e := ifd + 2 + i*12
		if e+12 > len(t) {
			return 1
		}
		if order.Uint16(t[e:e+2]) == 0x0112 {
			if v := int(order.Uint16(t[e+8 : e+10])); v >= 1 && v <= 8 {
				return v
			}
			return 1
		}
	}
	return 1
}

// swapsAxes reports whether an EXIF orientation turns the picture by a quarter.
func swapsAxes(orientation int) bool { return orientation >= 5 }

// orient returns src turned upright for an EXIF orientation: 2 mirrors it
// horizontally, 3 turns it 180 degrees, 4 mirrors it vertically, 5 and 7 are
// the two diagonal mirrors, 6 and 8 turn it a quarter clockwise and back.
func orient(src image.Image, orientation int) image.Image {
	if orientation < 2 || orientation > 8 {
		return src
	}
	b := src.Bounds()
	in := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(in, in.Bounds(), src, b.Min, draw.Src)
	w, h := in.Rect.Dx(), in.Rect.Dy()
	outW, outH := w, h
	if swapsAxes(orientation) {
		outW, outH = h, w
	}
	out := image.NewNRGBA(image.Rect(0, 0, outW, outH))
	for y := range h {
		for x := range w {
			var dx, dy int
			switch orientation {
			case 2:
				dx, dy = w-1-x, y
			case 3:
				dx, dy = w-1-x, h-1-y
			case 4:
				dx, dy = x, h-1-y
			case 5:
				dx, dy = y, x
			case 6:
				dx, dy = h-1-y, x
			case 7:
				dx, dy = h-1-y, w-1-x
			case 8:
				dx, dy = y, w-1-x
			}
			copy(out.Pix[dy*out.Stride+dx*4:dy*out.Stride+dx*4+4], in.Pix[y*in.Stride+x*4:y*in.Stride+x*4+4])
		}
	}
	return out
}
