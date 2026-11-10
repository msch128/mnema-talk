package imgstrip

import (
	"bytes"
	"encoding/binary"
	"io"

	"github.com/msch128/mnema-talk/internal/imgproc"
)

// WebP: a RIFF container ("RIFF", the length of what follows, "WEBP") of
// chunks (a four-letter type, a four-byte length, the data, a pad byte when
// the length is odd). Only the chunks that draw the picture stay: the image
// data (VP8, VP8L, ALPH), the extended header (VP8X), the animation (ANIM,
// ANMF) and the colour profile (ICCP). Every other chunk goes, whatever its
// name: "XMP ", content credentials, private chunks, and EXIF but for its
// orientation. The first chunk of the extended format, VP8X, starts with
// flags that announce EXIF and XMP; they agree with the chunks in the
// result. The file ends with the container.
const (
	webpHeaderLen = 12
	webpFlagsAt   = 20
	webpFlagXMP   = 0x04
	webpFlagEXIF  = 0x08
)

var webpKeeps = map[string]bool{"VP8 ": true, "VP8L": true, "VP8X": true, "ALPH": true, "ANIM": true, "ANMF": true, "ICCP": true}

// An animation frame (ANMF) is a header of webpFrameHeader bytes and chunks
// of its own. The ones that draw it stay. Any other is overwritten in both
// paths, since removing it would change the lengths around it: it becomes a
// "JUNK" chunk of zero bytes.
const webpFrameHeader = 16

var webpFrameKeeps = map[string]bool{"VP8 ": true, "VP8L": true, "ALPH": true}

// webpBlankFrame overwrites what frame, the data of an ANMF chunk as far as
// the file has it, holds besides its picture.
func webpBlankFrame(frame []byte) {
	pos := min(webpFrameHeader, len(frame))
	for len(frame)-pos >= 8 {
		size := uint64(binary.LittleEndian.Uint32(frame[pos+4 : pos+8]))
		end := int(min(uint64(len(frame)), uint64(pos)+8+size+size&1))
		if !webpFrameKeeps[string(frame[pos:pos+4])] {
			copy(frame[pos:], "JUNK")
			clear(frame[pos+8 : end])
		}
		pos = end
	}
	clear(frame[pos:])
}

// isWebP reports whether b starts a WebP whose container at least holds its
// own type.
func isWebP(b []byte) bool {
	return len(b) >= webpHeaderLen && string(b[:4]) == "RIFF" && string(b[8:12]) == "WEBP" && binary.LittleEndian.Uint32(b[4:8]) >= 4
}

// webpOrientationSource is the TIFF block of an EXIF chunk; some writers put
// the JPEG "Exif" header in front of it.
func webpOrientationSource(exif []byte) []byte { return bytes.TrimPrefix(exif, jpegExifHeader) }

// webpIsExtended reports whether b, a file from its start, opens with an
// extended header, which is where the flags are.
func webpIsExtended(b []byte) bool {
	return len(b) > webpFlagsAt && string(b[12:16]) == "VP8X" && binary.LittleEndian.Uint32(b[16:20]) > 0
}

// webpStart reads a WebP up to the buffer limit. One that fits is rewritten
// in the buffer, with its length corrected; a larger one streams.
func (f *filter) webpStart() error {
	data, err := f.webpRead()
	if err != nil {
		return err
	}
	if len(data) <= f.limit {
		f.out = stripWebP(data)
		return io.EOF
	}
	f.src = io.MultiReader(bytes.NewReader(data), f.src)
	f.step = f.webpHeader
	return nil
}

// webpRead reads the file up to one byte past the buffer limit. A small file
// stays in the block every filter has; a larger one gets one buffer of the
// limit's size, which is all the memory a WebP takes.
func (f *filter) webpRead() ([]byte, error) {
	head, err := f.take(min(blockSize, f.limit+1))
	switch {
	case err == io.EOF:
		return head, nil
	case err != nil:
		return nil, err
	case len(head) > f.limit:
		return bytes.Clone(head), nil
	}
	data := make([]byte, f.limit+1)
	n := copy(data, head)
	more, err := io.ReadFull(f.src, data[n:])
	if err = f.ended(err); err == io.EOF {
		err = nil
	}
	return data[:n+more], err
}

// webpRewrite compacts a WebP in its own buffer, which works because nothing
// in the result is longer than what it replaces: the chunks that stay move
// down over the ones that go.
type webpRewrite struct {
	data []byte
	// at is where the result ends so far.
	at uint64
	// extended: the result opens with an extended header, which has the flags.
	extended bool
	oriented bool
	keptEXIF bool
}

// keep moves a chunk, or the stump of one the file ends in, to the result.
// A frame goes without what it holds besides its picture, and an extended
// header that is not in front, where no decoder reads it, without the
// announcements the one in front may make.
func (w *webpRewrite) keep(chunk []byte) {
	first := w.at == webpHeaderLen
	switch kind := string(chunk[:4]); {
	case kind == "ANMF" && len(chunk) > 8:
		size := uint64(binary.LittleEndian.Uint32(chunk[4:8]))
		webpBlankFrame(chunk[8:min(uint64(len(chunk)), 8+size)])
	case kind == "VP8X" && len(chunk) > 8 && !first:
		chunk[8] &^= webpFlagXMP | webpFlagEXIF
	}
	w.at += uint64(copy(w.data[w.at:], chunk))
	if first {
		w.extended = webpIsExtended(w.data[:w.at])
	}
}

// exif writes the orientation of the file's first EXIF chunk back, if it
// turns the picture and the file has the header that can announce it.
func (w *webpRewrite) exif(payload []byte) {
	if w.oriented {
		return
	}
	w.oriented = true
	tiff := webpOrientationSource(payload[:min(len(payload), captureLimit)])
	if o := imgproc.TIFFOrientation(tiff); w.extended && turns(o) {
		record := orientationRecord(o, len(tiff))
		w.keptEXIF = true
		copy(w.data[w.at:], "EXIF")
		binary.LittleEndian.PutUint32(w.data[w.at+4:], uint32(len(record)))
		w.at += 8 + uint64(copy(w.data[w.at+8:], record))
	}
}

// stripWebP rewrites data in place and returns the file up to the end of its
// container, with the flags and the RIFF length corrected; data that is not
// a WebP is returned as it is.
//
// The orientation record is kept for a file with an extended header, and its
// flag set, also when the header did not announce the EXIF chunk: decoders
// read the chunk without the flag. A file without that header has no flags
// and gets no EXIF chunk, so its orientation is lost.
func stripWebP(data []byte) []byte {
	if !isWebP(data) {
		return data
	}
	declared := uint64(binary.LittleEndian.Uint32(data[4:8]))
	end := min(uint64(len(data)), 8+declared)
	w := &webpRewrite{data: data, at: webpHeaderLen}
	pos := uint64(webpHeaderLen)
	for pos+8 <= end {
		kind, size := string(data[pos:pos+4]), uint64(binary.LittleEndian.Uint32(data[pos+4:pos+8]))
		next := pos + 8 + size
		if next > end {
			break
		}
		if size&1 == 1 && next < end {
			next++
		}
		switch {
		case webpKeeps[kind]:
			w.keep(data[pos:next])
		case kind == "EXIF":
			w.exif(data[pos+8 : pos+8+size])
		}
		pos = next
	}
	// What is left is a chunk the file ends in; it stays if it is one that would.
	if webpKeeps[string(data[pos:min(pos+4, end)])] {
		w.keep(data[pos:end])
	}
	binary.LittleEndian.PutUint32(data[4:8], uint32(declared-(end-w.at)))
	if w.extended {
		data[webpFlagsAt] &^= webpFlagXMP | webpFlagEXIF
		if w.keptEXIF {
			data[webpFlagsAt] |= webpFlagEXIF
		}
	}
	return data[:w.at]
}
