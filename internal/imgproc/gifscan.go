// Package imgproc holds the pieces image uploads share: reading a GIF's
// structure before decoding it, redrawing its frames and encoding the result.
package imgproc

// GIFFrames walks the block structure of a GIF without decoding any
// pixels and returns the number of images, or 0 when the structure is broken.
// It stops counting past limit and lets the frame limit hold before the (memory hungry) decode.
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
		case 0x21: // extension: introducer and label, then data sub-blocks
			pos = skipSubBlocks(data, pos+2)
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
