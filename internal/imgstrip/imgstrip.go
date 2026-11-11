// Package imgstrip removes the metadata of JPEG, PNG and WebP files (EXIF with
// its GPS position, XMP, IPTC, comments, text and time chunks, embedded
// thumbnails and whatever follows the picture) without decoding or
// re-encoding it: the compressed image data, the colour information (ICC
// profile, gamma, chromaticities) and the frames of an animation pass through
// byte for byte. The orientation is the one thing a picture needs from its
// EXIF record to look the same, so it is written back as a record that holds
// nothing else.
//
// What stays is an allow-list in all three formats: a JPEG application
// segment, a PNG chunk or a WebP chunk the parser does not know is dropped.
//
// Two rules hold for every input. The result is never longer than the
// source. And the result ends where the picture ends: at the JPEG end marker,
// the PNG end chunk or the end of the WebP container.
//
// The parsers read member input. A file that does not start as its format
// does passes unchanged.
package imgstrip

import (
	"bufio"
	"errors"
	"io"

	"github.com/msch128/mnema-talk/internal/imgproc"
)

const (
	// blockSize is how much of a file is in memory at a time while it streams;
	// no JPEG segment is longer.
	blockSize = 64 << 10
	// captureLimit is how much of an EXIF record is read to find the
	// orientation; the tag sits in the first directory, at its start.
	captureLimit = blockSize
)

// An EXIF record with the orientation tag only: a TIFF header, a directory of
// one entry and the offset of the next directory (none). The value is the
// byte at orientationOffset. A source too short for the last four bytes gets
// the record without them, so a record never outgrows the one it replaces.
const (
	orientationTIFF   = "MM\x00\x2a\x00\x00\x00\x08" + "\x00\x01" + "\x01\x12\x00\x03\x00\x00\x00\x01\x00\x01\x00\x00" + "\x00\x00\x00\x00"
	orientationOffset = 19
	orientationMin    = len(orientationTIFF) - 4
	// emptyTIFF is an EXIF record without any tag.
	emptyTIFF = "MM\x00\x2a\x00\x00\x00\x08\x00\x00"
)

// orientationRecord returns the EXIF record that turns a picture as o does,
// at most room bytes long; room is at least orientationMin.
func orientationRecord(o, room int) []byte {
	t := []byte(orientationTIFF)
	t[orientationOffset] = byte(o)
	if room < len(t) {
		t = t[:orientationMin]
	}
	return t
}

// Reader returns src without the metadata of a picture of the type mimeType.
// JPEG and PNG stream through; a WebP of at most webpBuffer bytes is rewritten
// in memory, because its header states its length, and a larger one streams
// with its metadata overwritten in place (see webp_stream.go). Every other
// type is src itself.
func Reader(mimeType string, src io.Reader, webpBuffer int) io.Reader {
	if mimeType != "image/jpeg" && mimeType != "image/png" && mimeType != "image/webp" {
		return src
	}
	f := &filter{upload: &source{r: src}, limit: webpBuffer}
	f.src = f.upload
	switch mimeType {
	case "image/jpeg":
		f.bytes = bufio.NewReaderSize(f.src, blockSize)
		f.src, f.step = f.bytes, f.jpegStart
	case "image/png":
		f.step = f.pngStart
	default:
		f.step = f.webpStart
	}
	return f
}

// source is the upload. It remembers the error the upload failed with, which
// tells an upload that was cut off (it must fail) from a picture that is
// shorter than its own lengths say (it ends there).
type source struct {
	r   io.Reader
	err error
}

func (s *source) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	if err != nil && err != io.EOF {
		s.err = err
	}
	return n, err
}

// what happens to the bytes of a block (see filter.block).
const (
	keep = iota
	drop
	zero
)

// filter is a pull parser: each step reads a little of src and appends what
// stays to out, which Read hands on before the next step runs.
type filter struct {
	upload *source
	src    io.Reader
	// step is the parser's state: it produces output or ends the stream.
	step func() error
	out  []byte
	off  int
	err  error
	buf  [blockSize]byte

	// left bytes of the current block get mode, then the parser is at next.
	left uint64
	mode int
	next func() error

	// oriented: the first EXIF record was seen; later ones never count.
	oriented bool
	// PNG: exif is the orientation record to write once its source chunk ends.
	exif []byte
	// JPEG: bytes is src read byte by byte; sawFF says the last byte was a
	// 0xFF whose meaning the next one decides.
	bytes *bufio.Reader
	sawFF bool
	// inScan: the last segment was a start of scan, image data follows.
	inScan bool
	// WebP: limit is the largest file rewritten in memory. For a streamed one
	// container is what is left of its RIFF chunk, first says no chunk was
	// read yet and exifFlag that the header announces an EXIF chunk. Inside
	// an animation frame, frame is what is left of it, framePad its pad byte
	// and afterFrame the state that follows.
	limit      int
	container  uint64
	first      bool
	exifFlag   bool
	frame      uint64
	framePad   uint64
	afterFrame func() error
}

func (f *filter) Read(p []byte) (int, error) {
	for f.off == len(f.out) {
		if f.err != nil {
			return 0, f.err
		}
		f.out, f.off = f.out[:0], 0
		f.err = f.step()
	}
	n := copy(p, f.out[f.off:])
	f.off += n
	return n, nil
}

// take reads n bytes (at most blockSize) of the source. The slice is valid
// until the next take; it is shorter than n only together with an error,
// which is io.EOF when the source ended.
func (f *filter) take(n int) ([]byte, error) {
	got, err := io.ReadFull(f.src, f.buf[:n])
	return f.buf[:got], f.ended(err)
}

// ended turns the error of a read that got fewer bytes than it asked for
// into io.EOF: the file ends there. An upload that failed keeps its error.
func (f *filter) ended(err error) error {
	if errors.Is(err, io.ErrUnexpectedEOF) && f.upload.err == nil {
		return io.EOF
	}
	return err
}

func (f *filter) emit(b ...byte) { f.out = append(f.out, b...) }

// passThrough is the state for a file that is not what it was announced as.
func (f *filter) passThrough() error {
	n, err := f.src.Read(f.buf[:])
	f.emit(f.buf[:n]...)
	return err
}

// end is the state after the picture: what the source still holds is read,
// so an upload that fails or is too large still fails, and left out.
func (f *filter) end() error {
	if _, err := io.Copy(io.Discard, f.src); err != nil {
		return err
	}
	return io.EOF
}

// block is the state inside a segment or chunk: it keeps, drops or blanks
// the bytes that are left of it and then moves on to next.
func (f *filter) block() error {
	b, err := f.take(int(min(f.left, blockSize)))
	f.left -= uint64(len(b))
	switch f.mode {
	case keep:
		f.emit(b...)
	case zero:
		clear(b)
		f.emit(b...)
	}
	if f.left == 0 {
		f.step = f.next
	}
	return err
}

// firstOrientation returns the orientation (1 to 8) of an EXIF record when it
// is the file's first, else 0.
func (f *filter) firstOrientation(tiff []byte) int {
	if f.oriented {
		return 0
	}
	f.oriented = true
	return imgproc.TIFFOrientation(tiff)
}

// turns reports whether an orientation changes how a picture is shown; only
// then is a record worth keeping.
func turns(o int) bool { return o > 1 }
