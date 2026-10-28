package imgproc

import (
	"image"
	"image/color"
	"sort"
)

// maxOpaqueColours leaves one palette slot for the see-through pixel.
const maxOpaqueColours = 255

// adaptivePalette builds the palette of one composed frame by median cut over
// a histogram of its pixels (5 bits per channel; the bins keep the true mean
// colour), so a frame is drawn in its own colours rather than in whatever
// palette the source frame happened to carry. The result always ends with a
// fully transparent entry.
func adaptivePalette(img *image.NRGBA) color.Palette {
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
	type entry struct {
		n       int
		r, g, b float64 // mean colour
	}
	var entries []entry
	for _, h := range hist {
		if h.n > 0 {
			n := float64(h.n)
			entries = append(entries, entry{h.n, float64(h.r) / n, float64(h.g) / n, float64(h.b) / n})
		}
	}
	channel := func(e entry, c int) float64 { return [3]float64{e.r, e.g, e.b}[c] }
	boxes := [][]entry{entries}
	for len(boxes) < maxOpaqueColours {
		// Split the box that holds the most pixels and can still be split.
		best, bestN := -1, 0
		for i, box := range boxes {
			n := 0
			for _, e := range box {
				n += e.n
			}
			if len(box) > 1 && n > bestN {
				best, bestN = i, n
			}
		}
		if best < 0 {
			break
		}
		box := boxes[best]
		widest, widestRange := 0, -1.0
		for c := range 3 {
			lo, hi := channel(box[0], c), channel(box[0], c)
			for _, e := range box {
				lo, hi = min(lo, channel(e, c)), max(hi, channel(e, c))
			}
			if hi-lo > widestRange {
				widest, widestRange = c, hi-lo
			}
		}
		sort.Slice(box, func(i, j int) bool { return channel(box[i], widest) < channel(box[j], widest) })
		half, cum, cut := bestN/2, 0, 1
		for i, e := range box {
			cum += e.n
			if cum >= half {
				cut = min(max(i+1, 1), len(box)-1)
				break
			}
		}
		boxes[best] = box[:cut]
		boxes = append(boxes, box[cut:])
	}
	pal := make(color.Palette, 0, len(boxes)+1)
	for _, box := range boxes {
		var n, r, g, b float64
		for _, e := range box {
			w := float64(e.n)
			n, r, g, b = n+w, r+e.r*w, g+e.g*w, b+e.b*w
		}
		if n > 0 {
			pal = append(pal, color.NRGBA{R: uint8(r/n + 0.5), G: uint8(g/n + 0.5), B: uint8(b/n + 0.5), A: 255})
		}
	}
	return append(pal, color.Transparent)
}
