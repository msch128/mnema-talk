// Package imgproc holds the pieces image uploads share: reading a GIF's
// structure before decoding it, redrawing its frames and encoding the result.
package imgproc

// GIFFrames walks the block structure of a GIF without decoding any
// pixels and returns the number of images, or 0 when the structure is broken.
// It stops counting past limit and lets the frame limit hold before the (memory hungry) decode.
//
// The walk reads every block the way the decoder (image/gif) does, its
// deviations from the specification included, so that both always agree on
// where the next block starts: a file must not show the counter fewer images
// than the decoder goes on to decode.
func GIFFrames(data []byte, limit int) int {
	if len(data) < 13 {
		return 0
	}
	pos := 13 + colorTableSize(data[10])
	frames := 0
	for pos >= 0 && pos < len(data) {
		switch data[pos] {
		case 0x3B: // trailer
			return frames
		case 0x21: // extension
			pos = skipExtension(data, pos+1)
		case 0x2C: // image descriptor, optional local table, LZW size, data
			if pos+10 > len(data) {
				return 0
			}
			pos = skipSubBlocks(data, pos+10+colorTableSize(data[pos+9])+1)
			frames++
			if frames > limit {
				return frames
			}
		default:
			return 0
		}
	}
	return 0
}

// GIF extension labels.
const (
	gifPlainText      = 0x01
	gifGraphicControl = 0xF9
	gifComment        = 0xFE
	gifApplication    = 0xFF
)

// skipExtension returns the position after the extension whose label is at
// pos, or -1 when the data ends first or the decoder refuses the extension.
func skipExtension(data []byte, pos int) int {
	if pos >= len(data) {
		return -1
	}
	switch data[pos] {
	case gifPlainText:
		// The decoder takes 13 bytes for the header whatever its size byte says.
		return skipSubBlocks(data, pos+1+13)
	case gifGraphicControl:
		// A fixed block: size 4, four bytes, terminator. Nothing may follow it.
		if pos+7 > len(data) || data[pos+1] != 4 || data[pos+6] != 0 {
			return -1
		}
		return pos + 7
	case gifComment:
		return skipSubBlocks(data, pos+1)
	case gifApplication:
		// The header's size byte is not a sub-block: a zero there does not end
		// the extension, the sub-blocks still follow.
		if pos+1 >= len(data) {
			return -1
		}
		return skipSubBlocks(data, pos+2+int(data[pos+1]))
	}
	return -1
}

// colorTableSize is the byte length of the color table a packed field announces.
func colorTableSize(packed byte) int {
	if packed&0x80 == 0 {
		return 0
	}
	return 3 << (uint(packed&0x07) + 1)
}

// skipSubBlocks returns the position after the terminating empty sub-block,
// or -1 when the data ends first.
func skipSubBlocks(data []byte, pos int) int {
	for pos < len(data) {
		n := int(data[pos])
		if n == 0 {
			return pos + 1
		}
		pos += n + 1
	}
	return -1
}
