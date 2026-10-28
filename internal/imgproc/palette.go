package imgproc

import (
	"image"
	"image/color"
	"sort"
)

// maxOpaqueColours leaves one palette slot for the see-through pixel.
const maxOpaqueColours = 255

// colourEntry is the mean colour of the opaque pixels of one 5-bit-per-channel
// histogram cell, and how many pixels fell into it.
type colourEntry struct {
	n       int
	r, g, b float64
}

// colourEntries buckets the opaque pixels of img into a 5-bit-per-channel
// histogram and returns the mean colour of every occupied cell.
func colourEntries(img *image.NRGBA) []colourEntry {
	type bin struct {
		n       int
		r, g, b uint64
	}
	var hist [1 << 15]bin
	for y := img.Rect.Min.Y; y < img.Rect.Max.Y; y++ {
		row := img.Pix[(y-img.Rect.Min.Y)*img.Stride : (y-img.Rect.Min.Y)*img.Stride+img.Rect.Dx()*4]
		for i := 0; i+3 < len(row); i += 4 {
			if row[i+3] < 128 {
				continue // drawn with the transparent entry
			}
			r, g, b := row[i], row[i+1], row[i+2]
			h := &hist[int(r>>3)<<10|int(g>>3)<<5|int(b>>3)]
			h.n++
			h.r += uint64(r)
			h.g += uint64(g)
			h.b += uint64(b)
		}
	}
	var entries []colourEntry
	for _, h := range hist {
		if h.n > 0 {
			n := float64(h.n)
			entries = append(entries, colourEntry{h.n, float64(h.r) / n, float64(h.g) / n, float64(h.b) / n})
		}
	}
	return entries
}

// adaptivePalette builds the palette of one composed frame by median cut over
// a histogram of its pixels (5 bits per channel; the bins keep the true mean
// colour), so a frame is drawn in its own colours rather than in whatever
// palette the source frame happened to carry. The result always ends with a
// fully transparent entry.
func adaptivePalette(img *image.NRGBA) color.Palette {
	boxes := [][]colourEntry{colourEntries(img)}
	for len(boxes) < maxOpaqueColours {
		best, pixels := splittableBox(boxes)
		if best < 0 {
			break
		}
		box := boxes[best]
		cut := medianCut(box, pixels)
		boxes[best] = box[:cut]
		boxes = append(boxes, box[cut:])
	}
	pal := make(color.Palette, 0, len(boxes)+1)
	for _, box := range boxes {
		if c, ok := boxMean(box); ok {
			pal = append(pal, c)
		}
	}
	return append(pal, color.Transparent)
}

func (e colourEntry) channel(c int) float64 { return [3]float64{e.r, e.g, e.b}[c] }

// splittableBox picks the box that holds the most pixels and can still be
// split (-1 when none can) and returns its pixel count.
func splittableBox(boxes [][]colourEntry) (best, pixels int) {
	best = -1
	for i, box := range boxes {
		n := 0
		for _, e := range box {
			n += e.n
		}
		if len(box) > 1 && n > pixels {
			best, pixels = i, n
		}
	}
	return best, pixels
}

// medianCut sorts box along its widest colour channel and returns the index
// where the pixel count reaches half of pixels, always leaving both sides
// non-empty.
func medianCut(box []colourEntry, pixels int) int {
	widest, widestRange := 0, -1.0
	for c := range 3 {
		lo, hi := box[0].channel(c), box[0].channel(c)
		for _, e := range box {
			lo, hi = min(lo, e.channel(c)), max(hi, e.channel(c))
		}
		if hi-lo > widestRange {
			widest, widestRange = c, hi-lo
		}
	}
	sort.Slice(box, func(i, j int) bool { return box[i].channel(widest) < box[j].channel(widest) })
	half, cum := pixels/2, 0
	for i, e := range box {
		cum += e.n
		if cum >= half {
			return min(max(i+1, 1), len(box)-1)
		}
	}
	return 1
}

// boxMean is the pixel-weighted mean colour of a box; ok is false for an empty one.
func boxMean(box []colourEntry) (c color.NRGBA, ok bool) {
	var n, r, g, b float64
	for _, e := range box {
		w := float64(e.n)
		n, r, g, b = n+w, r+e.r*w, g+e.g*w, b+e.b*w
	}
	if n == 0 {
		return c, false
	}
	return color.NRGBA{R: uint8(r/n + 0.5), G: uint8(g/n + 0.5), B: uint8(b/n + 0.5), A: 255}, true
}
