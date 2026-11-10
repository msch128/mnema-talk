package imgstrip

import (
	"bytes"
	"encoding/binary"
	"image"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"testing/iotest"
)

// fuzzPixelBudget bounds the pictures a fuzz run decodes to compare them.
const fuzzPixelBudget = 1 << 16

// decodable returns the picture in data when it is one of the wanted format
// and small enough to compare.
func decodable(data []byte, format string) image.Image {
	cfg, got, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || got != format || cfg.Width*cfg.Height > fuzzPixelBudget {
		return nil
	}
	// Go finds a WebP by "VP8" where its first chunk is named and then skips
	// a chunk it does not know, so it draws a file that opens with a "VP8?"
	// chunk of any content. No WebP starts like that, no browser draws it,
	// and the filter treats the chunk like every other one it does not know.
	if format == "webp" && (len(data) < 16 || !webpKeeps[string(data[12:16])]) {
		return nil
	}
	// Go lets the last APP0 and the last APP14 segment of a JPEG say whether
	// it is JFIF and has Adobe's colour transform, also one that is neither.
	// libjpeg, which browsers draw with, goes by the ones it knows and skips
	// the others, and those are the ones that stay. Where a file has several,
	// the two decoders disagree about the upload itself.
	if format == "jpeg" && (bytes.Count(data, []byte{0xFF, jpegAPP0}) > 1 || bytes.Count(data, []byte{0xFF, jpegAPP14}) > 1) {
		return nil
	}
	m, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil
	}
	return m
}

// checkFilter holds for every input, picture or not:
//   - the filter ends without an error;
//   - the result is never longer than the input;
//   - input that does not start as the format does is unchanged;
//   - the result is the same whatever size the reads have;
//   - nothing is left to remove: filtering the result changes nothing, so no
//     metadata segment, trailing data or stale flag survived the first pass
//     (the targets also walk the result and name what may be in it);
//   - when the input is a picture, the result is the same picture.
//
// It returns the result.
func checkFilter(t *testing.T, mimeType, format, signature string, data []byte, webpBuffer int) []byte {
	out, err := io.ReadAll(Reader(mimeType, bytes.NewReader(data), webpBuffer))
	if err != nil {
		t.Fatalf("the filter failed: %v", err)
	}
	if len(out) > len(data) {
		t.Fatalf("%d bytes became %d", len(data), len(out))
	}
	if !strings.HasPrefix(string(data), signature) && !bytes.Equal(out, data) {
		t.Fatalf("input of another format changed: %q became %q", data, out)
	}
	slow, err := io.ReadAll(iotest.OneByteReader(Reader(mimeType, iotest.OneByteReader(bytes.NewReader(data)), webpBuffer)))
	if err != nil || !bytes.Equal(slow, out) {
		t.Fatalf("read byte by byte: %q (%v), in one go: %q", slow, err, out)
	}
	if again, err := io.ReadAll(Reader(mimeType, bytes.NewReader(out), webpBuffer)); err != nil || !bytes.Equal(again, out) {
		t.Fatalf("something was left to remove: %q became %q (%v)", out, again, err)
	}
	before := decodable(data, format)
	if before == nil {
		return out
	}
	after, _, err := image.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("the picture cannot be read any more: %v", err)
	}
	if !samePixels(before, after) {
		t.Fatal("the picture changed")
	}
	return out
}

func addSeeds(f *testing.F, files ...[]byte) {
	for _, file := range files {
		f.Add(file)
		f.Add(file[:len(file)/2])
		f.Add(append(bytes.Clone(file), file...))
	}
}

func FuzzJPEG(f *testing.F) {
	addSeeds(f, plainJPEG(f), phoneJPEG(f, 6),
		jpegWith(f, []byte{0xFF, 0xFF}, jpegSegment(0xE1, "Exif\x00\x00"+string(gpsTIFF(3)))),
		jpegWith(f, jpegSegment(0xE0, "JFIF\x00\x01\x01\x00\x00\x01\x00\x01\x01\x01abc"), jpegSegment(0xE0, "JFXX\x00\x10"), jpegSegment(0xE2, "MPF\x00"), jpegSegment(0xEE, "Adobe\x00\x64\x00\x00\x00\x00\x01more")),
		jpegWith(f, jpegSegment(0xE1, "Exif\x00\x00"+string(smallTIFF(6)))),
		append([]byte("\xff\xd8between"), jpegWith(f, []byte("segments"), jfifSegment, []byte{0xFF, 0x01, 0xFF, 0xD0})[2:]...),
		jpegWith(f, jpegSegment(0xF7, textMark), jpegSegment(0xC8, textMark), jpegSegment(0x02, textMark), jpegSegment(0xDD, "\x00\x00")),
		[]byte("\xff\xd8\xff\xe1\x00\x01"))
	// Progressive files and files with restart markers come with Go.
	samples, _ := filepath.Glob(filepath.Join(runtime.GOROOT(), "src", "image", "testdata", "video-001*.jpeg"))
	for _, sample := range samples {
		if data, err := os.ReadFile(sample); err == nil {
			f.Add(data)
		}
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		checkJPEGSegments(t, checkFilter(t, "image/jpeg", "jpeg", "\xff\xd8", data, wholeFile))
	})
}

func FuzzPNG(f *testing.F) {
	addSeeds(f, plainPNG(f), phonePNG(f, 6), pngWith(f, pngChunk("eXIf", string(smallTIFF(6)))),
		pngWith(f, pngChunk("caBX", textMark), pngChunk("TEXt", textMark), pngChunk("exIf", string(gpsTIFF(6))), pngChunk("sRGB", "\x00")), []byte(pngSignature+"\xff\xff\xff\xffeXIf"))
	f.Fuzz(func(t *testing.T, data []byte) {
		checkPNGChunks(t, checkFilter(t, "image/png", "png", pngSignature, data, wholeFile))
	})
}

func webpSeeds(f *testing.F) {
	addSeeds(f, plainWebP, phoneWebP(6), phoneWebP(1),
		webpOf(webpExtended(webpFlagEXIF), webpChunk("EXIF", "odd"), webpChunk("VP8L", webpPixel)),
		webpOf(webpExtended(0), webpChunk("VP8L", webpPixel), webpChunk("EXIF", string(smallTIFF(6)))),
		webpOf(webpChunk("JUNK", ""), webpExtended(webpFlagXMP|webpFlagEXIF), webpChunk("VP8L", webpPixel), webpChunk("EXIF", string(gpsTIFF(6)))),
		webpOf(webpExtended(0), webpChunk("GPSx", textMark), webpChunk("VP8L", webpPixel), webpChunk("xmp ", xmpPacket), webpChunk("XMP\x00", "odd")),
		webpOf(webpChunk("VP8L", webpPixel), webpChunk("EXIF", string(gpsTIFF(6)))),
		webpOf(webpExtended(0x02|webpFlagEXIF), webpChunk("ANIM", "\x00\x00\x00\x00\x00\x00"), webpFrame(webpChunk("EXIF", string(gpsTIFF(6))), webpChunk("VP8L", webpPixel), webpChunk("XMP ", "odd")), webpExtended(webpFlagXMP|webpFlagEXIF), webpChunk("EXIF", string(gpsTIFF(6)))),
		[]byte("RIFF\xff\xff\xff\xffWEBPEXIF\xff\xff\xff\xff"))
}

// orientationOnly reports whether an EXIF record holds the orientation tag
// and nothing else, or no tag at all; zero bytes may follow it.
func orientationOnly(record []byte) bool {
	set := bytes.TrimRight(record, "\x00")
	if len(set) == 0 || bytes.Equal(set, bytes.TrimRight([]byte(emptyTIFF), "\x00")) {
		return true
	}
	return len(set) == orientationOffset+1 && bytes.Equal(set[:orientationOffset], []byte(orientationTIFF[:orientationOffset])) && set[orientationOffset] <= 8
}

// jpegDrawsThePicture reports whether an application segment or comment is
// one of those a stored JPEG may hold.
func jpegDrawsThePicture(marker byte, payload []byte) bool {
	switch {
	case marker == jpegAPP0 && bytes.HasPrefix(payload, jfifHeader):
		return len(payload) < jfifLen || (len(payload) == jfifLen && payload[jfifLen-2] == 0 && payload[jfifLen-1] == 0)
	case marker == jpegAPP2:
		return bytes.HasPrefix(payload, iccHeader)
	case marker == jpegAPP14:
		return len(payload) == adobeLen && bytes.HasPrefix(payload, adobeHeader)
	case marker == jpegAPP1 && bytes.HasPrefix(payload, jpegExifHeader):
		return len(payload) >= len(jpegExifHeader)+orientationMin && orientationOnly(payload[len(jpegExifHeader):])
	}
	return false
}

// checkJPEGSegments: up to its first scan a stored JPEG is segments and
// nothing between them, and an application segment or comment among them is
// one that draws the picture.
func checkJPEGSegments(t *testing.T, out []byte) {
	if !bytes.HasPrefix(out, []byte("\xff\xd8")) {
		return
	}
	for pos := 2; pos < len(out); {
		if pos+2 > len(out) || out[pos] != 0xFF {
			t.Fatalf("bytes outside a segment at %d: %q", pos, out)
		}
		marker := out[pos+1]
		if marker == jpegSOS || (marker == jpegEOI && pos+2 == len(out)) {
			return
		}
		if jpegStandsAlone(marker) || marker == jpegEOI || pos+4 > len(out) {
			t.Fatalf("no segment at %d: %q", pos, out)
		}
		end := pos + 2 + int(binary.BigEndian.Uint16(out[pos+2:]))
		if !jpegDraws(marker) && (!jpegIsMetadata(marker) || end > len(out) || !jpegDrawsThePicture(marker, out[pos+4:end])) {
			t.Fatalf("a segment that does not draw the picture is left at %d: %q", pos, out)
		}
		pos = end
	}
}

// checkPNGChunks: a stored PNG holds the chunks that draw the picture, at
// most the orientation as EXIF, and nothing behind its end chunk.
func checkPNGChunks(t *testing.T, out []byte) {
	if !bytes.HasPrefix(out, []byte(pngSignature)) {
		return
	}
	for pos := len(pngSignature); pos < len(out); {
		if pos+8 > len(out) {
			t.Fatalf("half a chunk header at %d: %q", pos, out)
		}
		kind, end := string(out[pos+4:pos+8]), pos+12+int(binary.BigEndian.Uint32(out[pos:]))
		if !pngKeeps[kind] && (kind != "eXIf" || end > len(out) || end-pos-12 < orientationMin || !orientationOnly(out[pos+8:end-4])) {
			t.Fatalf("a %q chunk is left at %d: %q", kind, pos, out)
		}
		if kind == "IEND" && end < len(out) {
			t.Fatalf("data behind the end chunk: %q", out)
		}
		pos = end
	}
}

// checkWebPFrame: an animation frame holds its header, the chunks that draw
// it and JUNK chunks of zero bytes, where something else was.
func checkWebPFrame(t *testing.T, frame []byte) {
	pos := min(webpFrameHeader, len(frame))
	for len(frame)-pos >= 8 {
		kind, size := string(frame[pos:pos+4]), int(binary.LittleEndian.Uint32(frame[pos+4:]))
		end := min(len(frame), pos+8+size+size%2)
		if !webpFrameKeeps[kind] && (kind != "JUNK" || len(bytes.TrimRight(frame[pos+8:end], "\x00")) != 0) {
			t.Fatalf("a %q chunk is left in a frame: %q", kind, frame)
		}
		pos = end
	}
	if len(bytes.TrimRight(frame[pos:], "\x00")) != 0 {
		t.Fatalf("a frame ends in %q", frame[pos:])
	}
}

// checkWebPChunks: a stored WebP holds the chunks that draw the picture and
// at most the orientation as EXIF; a streamed one also JUNK chunks of zero
// bytes, where something was. Only the extended header in front announces
// anything. The header never announces XMP, and an EXIF
// chunk is there only when the header announces it (always then, unless
// streamed: a rewritten file knows what it holds, a streamed one has written
// its header before it finds out).
func checkWebPChunks(t *testing.T, out []byte, streamed bool) {
	if !isWebP(out) {
		return
	}
	extended := webpIsExtended(out)
	announced := extended && out[webpFlagsAt]&webpFlagEXIF != 0
	chunks, end := 0, min(len(out), 8+int(binary.LittleEndian.Uint32(out[4:8])))
	for pos := webpHeaderLen; pos+8 <= end; {
		kind, size := string(out[pos:pos+4]), int(binary.LittleEndian.Uint32(out[pos+4:]))
		payload := out[pos+8 : min(pos+8+size, end)]
		switch {
		case kind == "ANMF":
			checkWebPFrame(t, payload)
		case kind == "VP8X" && pos > webpHeaderLen && len(payload) > 0 && payload[0]&(webpFlagXMP|webpFlagEXIF) != 0:
			t.Fatalf("an extended header at %d announces metadata: %q", pos, out)
		case webpKeeps[kind]:
		case kind == "EXIF" && orientationOnly(payload):
			chunks++
		case kind == "JUNK" && streamed && len(bytes.TrimRight(payload, "\x00")) == 0:
		default:
			t.Fatalf("a %q chunk is left at %d: %q", kind, pos, out)
		}
		pos += 8 + size + size%2
	}
	if extended && out[webpFlagsAt]&webpFlagXMP != 0 {
		t.Fatalf("XMP is still announced: %q", out)
	}
	if chunks > 1 || (chunks == 1 && !announced) || (!streamed && announced && chunks == 0) {
		t.Fatalf("EXIF announced: %v, EXIF chunks: %d in %q", announced, chunks, out)
	}
}

// FuzzWebP rewrites the file in memory, as an upload within the buffer is.
func FuzzWebP(f *testing.F) {
	webpSeeds(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			return
		}
		checkWebPChunks(t, checkFilter(t, "image/webp", "webp", "RIFF", data, len(data)), false)
	})
}

// FuzzWebPStream overwrites the metadata in place, as for an upload larger
// than the buffer.
func FuzzWebPStream(f *testing.F) {
	webpSeeds(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			return
		}
		checkWebPChunks(t, checkFilter(t, "image/webp", "webp", "RIFF", data, noBuffer), true)
	})
}
