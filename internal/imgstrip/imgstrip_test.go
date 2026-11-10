package imgstrip

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"testing/iotest"

	_ "golang.org/x/image/webp"
)

// noBuffer makes every WebP stream; wholeFile lets every fixture fit.
const (
	noBuffer  = 0
	wholeFile = 1 << 20
)

func strip(t testing.TB, mimeType string, data []byte, webpBuffer int) []byte {
	t.Helper()
	out, err := io.ReadAll(Reader(mimeType, bytes.NewReader(data), webpBuffer))
	if err != nil {
		t.Fatalf("filtering %s: %v", mimeType, err)
	}
	return out
}

func pixels(t testing.TB, data []byte) image.Image {
	t.Helper()
	m, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("the picture cannot be read: %v", err)
	}
	return m
}

func samePixels(a, b image.Image) bool {
	if a.Bounds() != b.Bounds() {
		return false
	}
	for y := a.Bounds().Min.Y; y < a.Bounds().Max.Y; y++ {
		for x := a.Bounds().Min.X; x < a.Bounds().Max.X; x++ {
			if a.At(x, y) != b.At(x, y) {
				return false
			}
		}
	}
	return true
}

type phoneFile struct {
	mime   string
	buffer int
	build  func(t testing.TB, orientation int) []byte
}

var phoneFiles = map[string]phoneFile{
	"jpeg":          {"image/jpeg", wholeFile, phoneJPEG},
	"png":           {"image/png", wholeFile, phonePNG},
	"webp":          {"image/webp", wholeFile, func(_ testing.TB, o int) []byte { return phoneWebP(o) }},
	"streamed webp": {"image/webp", noBuffer, func(_ testing.TB, o int) []byte { return phoneWebP(o) }},
}

func TestAPhotoLosesItsMetadataAndKeepsItsPicture(t *testing.T) {
	for name, file := range phoneFiles {
		t.Run(name, func(t *testing.T) {
			in := file.build(t, 6)
			for _, mark := range []string{camMark, xmpMark, iccMark} {
				if !bytes.Contains(in, []byte(mark)) {
					t.Fatalf("the fixture lacks %q", mark)
				}
			}
			out := strip(t, file.mime, in, file.buffer)
			for _, mark := range metadataMarks {
				if bytes.Contains(out, []byte(mark)) {
					t.Errorf("%q is still in the file", mark)
				}
			}
			if !bytes.Contains(out, []byte(iccMark)) {
				t.Error("the colour profile is gone")
			}
			if !samePixels(pixels(t, in), pixels(t, out)) {
				t.Error("the picture changed")
			}
			if records := exifRecords(out); len(records) != 1 || !bytes.HasPrefix(records[0], orientationRecord(6, len(orientationTIFF))) || strings.Trim(string(records[0][len(orientationTIFF):]), "\x00") != "" {
				t.Errorf("EXIF left in the file: %q, want the orientation and nothing else", records)
			}
		})
	}
}

// A second EXIF record does not decide how the picture is turned, so it must
// not become the one that is kept.
func TestOnlyTheFirstEXIFRecordCounts(t *testing.T) {
	upright, turned := "Exif\x00\x00"+string(gpsTIFF(1)), "Exif\x00\x00"+string(gpsTIFF(8))
	files := map[string][]byte{
		"image/jpeg": jpegWith(t, jpegSegment(0xE1, upright), jpegSegment(0xE1, turned)),
		"image/png":  pngWith(t, pngChunk("eXIf", string(gpsTIFF(1))), pngChunk("eXIf", string(gpsTIFF(8)))),
		"image/webp": webpOf(webpExtended(webpFlagEXIF), webpChunk("VP8L", webpPixel), webpChunk("EXIF", upright), webpChunk("EXIF", turned)),
	}
	for mimeType, in := range files {
		for _, buffer := range []int{wholeFile, noBuffer} {
			if out := strip(t, mimeType, in, buffer); shownOrientation(out) != 1 || bytes.Contains(out, []byte(camMark)) {
				t.Errorf("%s: orientation %d, records %q", mimeType, shownOrientation(out), exifRecords(out))
			}
		}
	}
}

func TestAFileWithoutMetadataIsNotChanged(t *testing.T) {
	files := map[string][]byte{
		"image/jpeg":      jpegWith(t, jfifSegment, iccSegment, adobeSegment),
		"image/png":       pngWith(t, gammaChunk, iccChunk),
		"image/webp":      webpOf(webpExtended(0x20), webpICC, webpChunk("VP8L", webpPixel)),
		"image/gif":       []byte("GIF89a\x01\x00\x01\x00\x80\x00\x00"),
		"application/pdf": []byte("%PDF-1.7 Exif\x00\x00 not a picture"),
		"text/plain":      []byte("\xff\xd8\xff\xe1 looks like a photo, is declared as text"),
	}
	for mimeType, in := range files {
		for _, buffer := range []int{wholeFile, noBuffer} {
			if out := strip(t, mimeType, in, buffer); !bytes.Equal(out, in) {
				t.Errorf("%s changed:\n in  %q\n out %q", mimeType, in, out)
			}
		}
	}
	if plain := webpOf(webpChunk("VP8L", webpPixel)); !bytes.Equal(plain, plainWebP) || !samePixels(pixels(t, plain), pixels(t, phoneWebP(1))) {
		t.Fatal("the WebP fixtures do not hold the same picture")
	}
}

// Bytes that are not the format they were announced as pass as they are.
func TestBytesOfAnotherFormatPassUnchanged(t *testing.T) {
	in := []byte("no picture at all, just text that is long enough to hold a header")
	for _, mimeType := range []string{"image/jpeg", "image/png", "image/webp"} {
		for _, buffer := range []int{wholeFile, noBuffer} {
			if out := strip(t, mimeType, in, buffer); !bytes.Equal(out, in) {
				t.Errorf("%s: %q", mimeType, out)
			}
		}
	}
}

func TestAnAnimationKeepsItsFrames(t *testing.T) {
	frames := []byte(string(pngChunk("acTL", "\x00\x00\x00\x02\x00\x00\x00\x00")) + string(pngChunk("fcTL", strings.Repeat("\x00", 26))))
	second := pngChunk("fdAT", "\x00\x00\x00\x01frame-two")
	plain := plainPNG(t)
	apng := pngWith(t, frames, pngChunk("tEXt", "Comment\x00"+textMark))
	apng = append(append(bytes.Clone(apng[:len(apng)-12]), second...), plain[len(plain)-12:]...)
	out := strip(t, "image/png", apng, wholeFile)
	if !bytes.Contains(out, frames) || !bytes.Contains(out, second) || bytes.Contains(out, []byte(textMark)) {
		t.Errorf("animated PNG: %q", out)
	}

	animation := string(webpChunk("ANIM", "\x00\x00\x00\x00\x00\x00")) + string(webpChunk("ANMF", strings.Repeat("\x00", 16)+string(webpChunk("VP8L", webpPixel)))) + string(webpChunk("ANMF", strings.Repeat("\x00", 16)+string(webpChunk("VP8L", webpPixel))))
	animated := webpOf(webpExtended(0x02|webpFlagXMP), []byte(animation), webpChunk("XMP ", xmpPacket))
	for _, buffer := range []int{wholeFile, noBuffer} {
		out := strip(t, "image/webp", animated, buffer)
		if !bytes.Contains(out, []byte(animation)) || out[webpFlagsAt] != 0x02 || bytes.Contains(out, []byte(xmpMark)) {
			t.Errorf("animated WebP (buffer %d): %q", buffer, out)
		}
	}
}

func TestABufferedWebPStatesItsNewLength(t *testing.T) {
	out := strip(t, "image/webp", phoneWebP(6), wholeFile)
	if got := int(binary.LittleEndian.Uint32(out[4:8])); got != len(out)-8 {
		t.Errorf("RIFF length %d, file has %d bytes after it", got, len(out)-8)
	}
	if flags := out[webpFlagsAt]; flags != 0x20|webpFlagEXIF {
		t.Errorf("flags %#x, want the colour profile and the orientation record announced", flags)
	}
	if upright := strip(t, "image/webp", phoneWebP(1), wholeFile); upright[webpFlagsAt] != 0x20 {
		t.Errorf("flags %#x, want no EXIF announced once it is gone", upright[webpFlagsAt])
	}
}

// A WebP too large to buffer keeps its length: its metadata is overwritten.
func TestAStreamedWebPKeepsItsLength(t *testing.T) {
	in := phoneWebP(6)
	out := strip(t, "image/webp", in, len(in)-1)
	if len(out) != len(in) || !bytes.Contains(out, []byte("JUNK")) || bytes.Contains(out, []byte("XMP ")) {
		t.Errorf("%d bytes in, %d out: %q", len(in), len(out), out)
	}
	if out[webpFlagsAt]&webpFlagXMP != 0 {
		t.Error("XMP is still announced")
	}
}

// The filter sits between the upload and storage: an upload that fails or is
// cut off at the size limit has to fail there too.
func TestAFailingUploadFailsTheFilter(t *testing.T) {
	files := map[string][]byte{"image/jpeg": phoneJPEG(t, 6), "image/png": phonePNG(t, 6), "image/webp": phoneWebP(6), "image/gif": []byte("GIF89a")}
	// A form part without its closing boundary ends with io.ErrUnexpectedEOF,
	// which is also what reading a file that is shorter than it says gives.
	for _, cut := range []error{errors.New("upload cut off"), io.ErrUnexpectedEOF} {
		for mimeType, in := range files {
			for _, buffer := range []int{wholeFile, noBuffer} {
				for _, at := range []int{0, 3, len(in) / 2, 2 * len(in) / 3, len(in)} {
					src := io.MultiReader(bytes.NewReader(in[:at]), iotest.ErrReader(cut))
					if _, err := io.ReadAll(Reader(mimeType, src, buffer)); !errors.Is(err, cut) {
						t.Errorf("%s cut at %d: err = %v, want %v", mimeType, at, err, cut)
					}
				}
			}
		}
	}
}

func TestAPictureIsShownTheWayItWasSent(t *testing.T) {
	for name, file := range phoneFiles {
		for orientation := 1; orientation <= 8; orientation++ {
			out := strip(t, file.mime, file.build(t, orientation), file.buffer)
			if got := shownOrientation(out); got != orientation {
				t.Errorf("%s sent with orientation %d is stored with %d", name, orientation, got)
			}
			// A streamed WebP has announced its EXIF chunk by the time it is read.
			if orientation == 1 && len(exifRecords(out)) != 0 && name != "streamed webp" {
				t.Errorf("%s: an upright picture needs no EXIF record", name)
			}
		}
	}
}

// Only what a decoder needs to draw the picture stays in a JPEG, in the form
// the decoder reads: a thumbnail is a second copy of the photo with metadata
// of its own, and multi-picture or FlashPix data point at more of them.
func TestAJPEGKeepsOnlyWhatDrawsThePicture(t *testing.T) {
	icc1, icc2 := jpegSegment(0xE2, "ICC_PROFILE\x00\x01\x02"+iccMark), jpegSegment(0xE2, "ICC_PROFILE\x00\x02\x02\xff\xd9 second part")
	in := jpegWith(t,
		jpegSegment(0xE0, "JFIF\x00\x01\x01\x00\x00\x01\x00\x01\x02\x02"+thumbMark),
		jpegSegment(0xE0, "JFXX\x00\x10\xff\xd8"+thumbMark),
		icc1, icc2,
		jpegSegment(0xE2, "MPF\x00MM\x00\x2a"+thumbMark),
		jpegSegment(0xE2, "FPXR\x00"+textMark),
		jpegSegment(0xE2, textMark),
		jpegSegment(0xEE, "Adobe\x00\x64\x00\x00\x00\x00\x01"+textMark),
		jpegSegment(0xEE, "Other\x00"+textMark),
		jpegSegment(0xEB, "JP\x00\x01"+iptcMark),
	)
	out := strip(t, "image/jpeg", in, wholeFile)
	want := jpegWith(t, jfifSegment, icc1, icc2, adobeSegment)
	if !bytes.Equal(out, want) {
		t.Errorf("stored:\n %q\nwant:\n %q", out, want)
	}
	if !samePixels(pixels(t, in), pixels(t, out)) {
		t.Error("the picture changed")
	}
}

// The stored file ends where the picture ends: a second picture, the video
// of a motion photo, an archive or more metadata behind it are left out.
func TestTheFileEndsWithThePicture(t *testing.T) {
	trailer := "\xff\xe1\x00\x10Exif\x00\x00" + camMark + string(phoneJPEG(t, 8)) + string(pngChunk("tEXt", textMark)) + string(webpChunk("EXIF", string(gpsTIFF(8))))
	files := map[string][]byte{"image/jpeg": phoneJPEG(t, 3), "image/png": phonePNG(t, 3), "image/webp": phoneWebP(3)}
	for mimeType, in := range files {
		for _, buffer := range []int{wholeFile, noBuffer} {
			out := strip(t, mimeType, append(bytes.Clone(in), trailer...), buffer)
			if want := strip(t, mimeType, in, buffer); !bytes.Equal(out, want) || shownOrientation(out) != 3 {
				t.Errorf("%s (buffer %d): %d bytes stored, %d without the trailer", mimeType, buffer, len(out), len(want))
			}
		}
	}
}

// The end of a JPEG is the end marker that follows its last scan: neither the
// same two bytes inside a segment nor the first of several scans.
func TestAJPEGEndsAtItsEndMarker(t *testing.T) {
	in := jpegWith(t, jpegSegment(0xE2, "ICC_PROFILE\x00\x01\x01\xff\xd9"+iccMark), jpegSegment(0xFE, "\xff\xd9"+textMark))
	out := strip(t, "image/jpeg", append(bytes.Clone(in), camMark...), wholeFile)
	if !bytes.Contains(out, []byte(iccMark)) || !bytes.HasSuffix(out, []byte("\xff\xd9")) || !samePixels(pixels(t, in), pixels(t, out)) {
		t.Errorf("stored: %q", out)
	}

	// Progressive files and files with restart markers come with Go.
	samples, _ := filepath.Glob(filepath.Join(runtime.GOROOT(), "src", "image", "testdata", "video-001*.jpeg"))
	if len(samples) == 0 {
		t.Skip("the Go sources with their sample pictures are not installed")
	}
	for _, sample := range samples {
		plain, err := os.ReadFile(sample)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := image.Decode(bytes.NewReader(plain)); err != nil {
			continue
		}
		photo := append(append([]byte{0xFF, 0xD8}, jpegSegment(0xE1, "Exif\x00\x00"+string(gpsTIFF(6)))...), plain[2:]...)
		out := strip(t, "image/jpeg", append(photo, phoneJPEG(t, 8)...), wholeFile)
		if !samePixels(pixels(t, plain), pixels(t, out)) || bytes.Contains(out, []byte(camMark)) || shownOrientation(out) != 6 {
			t.Errorf("%s: the picture or its orientation changed, or metadata stayed", filepath.Base(sample))
		}
		if !bytes.HasSuffix(out, []byte("\xff\xd9")) || len(out) > len(plain)+len(jpegSegment(0xE1, "Exif\x00\x00"+orientationTIFF)) {
			t.Errorf("%s: %d bytes stored of a picture of %d", filepath.Base(sample), len(out), len(plain))
		}
	}
}

// In a WebP the header's flags and the chunks agree, also when the file is
// too large to rewrite: no XMP, and an EXIF chunk exactly where the header
// announces one. A rewritten file with an extended header keeps the
// orientation of an EXIF chunk its header did not announce; a streamed one
// and one without that header cannot.
func TestAWebPAnnouncesWhatItHolds(t *testing.T) {
	exif, xmp, turned := webpChunk("EXIF", string(gpsTIFF(1))), webpChunk("XMP ", xmpPacket), webpChunk("EXIF", string(gpsTIFF(6)))
	cases := map[string]struct {
		in     []byte
		buffer int
		flags  byte
		shown  int // 0: no EXIF chunk
	}{
		"buffered, upright":              {webpOf(webpExtended(webpFlagEXIF|webpFlagXMP), webpChunk("VP8L", webpPixel), exif, xmp), wholeFile, 0, 0},
		"streamed, upright":              {webpOf(webpExtended(webpFlagEXIF|webpFlagXMP), webpChunk("VP8L", webpPixel), exif, xmp), noBuffer, webpFlagEXIF, 1},
		"streamed, tiny EXIF chunk":      {webpOf(webpExtended(webpFlagEXIF), webpChunk("VP8L", webpPixel), webpChunk("EXIF", "MM\x00\x2a\x00\x00\x00\x08\x00\x00"+"ab")), noBuffer, webpFlagEXIF, 1},
		"buffered, EXIF not announced":   {webpOf(webpExtended(0), webpChunk("VP8L", webpPixel), turned), wholeFile, webpFlagEXIF, 6},
		"streamed, EXIF not announced":   {webpOf(webpExtended(0), webpChunk("VP8L", webpPixel), turned), noBuffer, 0, 0},
		"buffered, no extended header":   {webpOf(webpChunk("VP8L", webpPixel), turned), wholeFile, 0, 0},
		"streamed, no extended header":   {webpOf(webpChunk("VP8L", webpPixel), turned), noBuffer, 0, 0},
		"buffered, flag without a chunk": {webpOf(webpExtended(webpFlagEXIF), webpChunk("VP8L", webpPixel), xmp), wholeFile, 0, 0},
		"buffered, header not in front":  {webpOf(webpChunk("GPSx", textMark), webpExtended(webpFlagEXIF|webpFlagXMP), webpChunk("VP8L", webpPixel), turned), wholeFile, webpFlagEXIF, 6},
	}
	for name, c := range cases {
		out := strip(t, "image/webp", c.in, c.buffer)
		flags := byte(0)
		if string(out[12:16]) == "VP8X" {
			flags = out[webpFlagsAt]
		}
		records := exifRecords(out)
		if flags != c.flags || (len(records) == 1) != (c.shown != 0) || bytes.Contains(out, []byte("XMP ")) || bytes.Contains(out, []byte(camMark)) {
			t.Errorf("%s: flags %#x, EXIF chunks %q", name, flags, records)
		}
		if c.shown != 0 && shownOrientation(out) != c.shown {
			t.Errorf("%s: orientation %d, want %d", name, shownOrientation(out), c.shown)
		}
		if again := strip(t, "image/webp", out, c.buffer); !bytes.Equal(again, out) {
			t.Errorf("%s: stored again it changes:\n %q\n %q", name, out, again)
		}
	}
}

// What stays in a PNG is a list: a chunk that is not on it goes, whatever it
// is called. Content credentials, the private chunks of editors and a text
// chunk under a name in other case all carry what a text chunk carries.
func TestAPNGKeepsOnlyWhatDrawsThePicture(t *testing.T) {
	var kept, dropped [][]byte
	for _, kind := range []string{"cHRM", "gAMA", "iCCP", "sBIT", "sRGB", "cICP", "mDCV", "cLLI", "bKGD", "hIST", "pHYs", "sPLT", "tRNS"} {
		kept = append(kept, pngChunk(kind, iccMark+kind))
	}
	for _, kind := range []string{"caBX", "prVW", "mkBS", "zxIf", "tXMP", "gIFt", "gIFx", "dSIG", "vpAg", "meTa", "iDOT", "orNT", "prVt", "tEXT", "text", "TEXt", "teXt", "itXt", "exIf", "tIME", "JUNK"} {
		dropped = append(dropped, pngChunk(kind, textMark+kind))
	}
	mixed := make([][]byte, 0, len(kept)+len(dropped))
	for i := range max(len(kept), len(dropped)) {
		if i < len(dropped) {
			mixed = append(mixed, dropped[i])
		}
		if i < len(kept) {
			mixed = append(mixed, kept[i])
		}
	}
	if out, want := strip(t, "image/png", pngWith(t, mixed...), wholeFile), pngWith(t, kept...); !bytes.Equal(out, want) {
		t.Errorf("stored:\n %q\nwant:\n %q", out, want)
	}
	in := pngWith(t, dropped...)
	if out := strip(t, "image/png", in, wholeFile); !bytes.Equal(out, plainPNG(t)) || !samePixels(pixels(t, in), pixels(t, out)) {
		t.Errorf("stored: %q", out)
	}
}

// The same for a WebP. A rewritten file loses the chunk; a streamed one keeps
// its length, so the chunk is blanked and renamed.
func TestAWebPKeepsOnlyWhatDrawsThePicture(t *testing.T) {
	unknown := []string{"GPSx", "exif", "xmp ", "XMP\x00", "C2PA", "JUMB", "JUNK", "vp8l"}
	chunks := [][]byte{webpExtended(0x20), webpICC}
	for i, kind := range unknown {
		if i == len(unknown)/2 {
			chunks = append(chunks, webpChunk("VP8L", webpPixel))
		}
		chunks = append(chunks, webpChunk(kind, textMark+kind))
	}
	in, want := webpOf(chunks...), webpOf(webpExtended(0x20), webpICC, webpChunk("VP8L", webpPixel))
	if out := strip(t, "image/webp", in, wholeFile); !bytes.Equal(out, want) {
		t.Errorf("stored:\n %q\nwant:\n %q", out, want)
	}
	out := strip(t, "image/webp", in, noBuffer)
	if len(out) != len(in) || bytes.Contains(out, []byte(textMark)) || bytes.Count(out, []byte("JUNK")) != len(unknown) || !bytes.Contains(out, webpICC) {
		t.Errorf("streamed: %q", out)
	}
	for _, kind := range unknown[:len(unknown)-2] {
		if bytes.Contains(out, []byte(kind)) {
			t.Errorf("streamed: a %q chunk is left", kind)
		}
	}
	if !samePixels(pixels(t, in), pixels(t, out)) {
		t.Error("the streamed picture changed")
	}
}

// A JPEG segment stays when a decoder draws from it. One under a reserved
// marker or a marker of a JPEG extension holds what a comment holds.
func TestAJPEGKeepsNoSegmentADecoderDoesNotRead(t *testing.T) {
	var unread [][]byte
	for _, marker := range []byte{0x02, 0x4F, 0xBF, 0xC8, 0xF0, 0xF7, 0xFD} {
		unread = append(unread, jpegSegment(marker, textMark))
	}
	restart := jpegSegment(0xDD, "\x00\x00")
	in := jpegWith(t, append(append([][]byte{jfifSegment}, unread...), restart)...)
	if out, want := strip(t, "image/jpeg", in, wholeFile), jpegWith(t, jfifSegment, restart); !bytes.Equal(out, want) {
		t.Errorf("stored:\n %q\nwant:\n %q", out, want)
	}
	// Each sample of the Go sources (baseline, progressive, restart markers)
	// has every segment it came with.
	samples, _ := filepath.Glob(filepath.Join(runtime.GOROOT(), "src", "image", "testdata", "video-00*.jpeg"))
	for _, sample := range samples {
		plain, err := os.ReadFile(sample)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := image.Decode(bytes.NewReader(plain)); err != nil {
			continue
		}
		if out := strip(t, "image/jpeg", plain, wholeFile); !samePixels(pixels(t, plain), pixels(t, out)) {
			t.Errorf("%s: the picture changed", filepath.Base(sample))
		}
	}
}

// The frames of an animated WebP are chunks that hold chunks. What a frame
// holds besides its picture is blanked in both paths (the lengths around it
// stay), and only the extended header in front announces anything.
func TestAnAnimationFrameKeepsOnlyItsPicture(t *testing.T) {
	anim, picture := webpChunk("ANIM", "\x00\x00\x00\x00\x00\x00"), webpChunk("VP8L", webpPixel)
	frame := func(kinds ...string) []byte {
		chunks := [][]byte{webpChunk("ALPH", "a"), picture}
		for _, kind := range kinds {
			chunks = append(chunks, webpChunk(kind, strings.Repeat("\x00", len(textMark)+1)))
			if kind != "JUNK" {
				chunks[len(chunks)-1] = webpChunk(kind, textMark+"!")
			}
		}
		return webpFrame(chunks...)
	}
	late := func(flags byte) []byte { return webpExtended(flags) }
	in := webpOf(webpExtended(0x02|webpFlagXMP), anim, frame("GPSx", "EXIF", "XMP "), late(webpFlagXMP|webpFlagEXIF|0x02), frame(), webpChunk("XMP ", xmpPacket))
	blank := []byte(string(webpExtended(0x02)) + string(anim) + string(frame("JUNK", "JUNK", "JUNK")) + string(late(0x02)) + string(frame()))
	if out, want := strip(t, "image/webp", in, wholeFile), webpOf(blank); !bytes.Equal(out, want) {
		t.Errorf("stored:\n %q\nwant:\n %q", out, want)
	}
	out := strip(t, "image/webp", in, noBuffer)
	if len(out) != len(in) || !bytes.Equal(out[webpHeaderLen:webpHeaderLen+len(blank)], blank) || bytes.Contains(out, []byte(xmpMark)) {
		t.Errorf("streamed:\n %q\nwant it to hold:\n %q", out, blank)
	}
	for cut := range len(in) {
		for _, buffer := range []int{wholeFile, noBuffer} {
			if out := strip(t, "image/webp", in[:cut], buffer); bytes.Contains(out, []byte(textMark)) || len(out) > cut {
				t.Fatalf("cut at %d (buffer %d): %q", cut, buffer, out)
			}
		}
	}
}

// Up to its image data a JPEG is segments. What a file has between them is
// not read by a decoder and is not stored.
func TestAJPEGHasNothingBetweenItsSegments(t *testing.T) {
	plain := plainJPEG(t)
	in := []byte("\xff\xd8" + camMark + string(jfifSegment) + textMark + "\xff\x01\xff\xd0" + iptcMark + string(plain[2:]))
	out := strip(t, "image/jpeg", in, wholeFile)
	if want := jpegWith(t, jfifSegment); !bytes.Equal(out, want) {
		t.Errorf("stored:\n %q\nwant:\n %q", out, want)
	}
	if !samePixels(pixels(t, plain), pixels(t, out)) {
		t.Error("the picture changed")
	}
}

// A WebP is rewritten in one buffer of the limit's size: an upload costs that
// much memory and no more. Just above the limit it streams.
func TestABufferedWebPNeedsOneBufferOfItsLimit(t *testing.T) {
	const limit = 1 << 20
	picture := webpChunk("VP8L", webpPixel)
	in := webpOf(picture, webpChunk("XMP ", xmpPacket+strings.Repeat("x", limit-len(picture)-len(xmpPacket)-20)))
	if len(in) != limit {
		t.Fatalf("the fixture has %d bytes, want the limit", len(in))
	}
	if out := strip(t, "image/webp", in, limit); !bytes.Equal(out, plainWebP) {
		t.Errorf("at the limit: %d bytes stored, want the picture alone", len(out))
	}
	if out := strip(t, "image/webp", in, limit-1); len(out) != limit || bytes.Contains(out, []byte(xmpMark)) || !bytes.HasPrefix(out[len(plainWebP):], []byte("JUNK")) {
		t.Errorf("above the limit: %d bytes stored, want the length kept and the chunk blanked", len(out))
	}

	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	if _, err := io.Copy(io.Discard, Reader("image/webp", bytes.NewReader(in), limit)); err != nil {
		t.Fatal(err)
	}
	runtime.ReadMemStats(&after)
	if used := after.TotalAlloc - before.TotalAlloc; used > limit+limit/2 {
		t.Errorf("rewriting a WebP of %d bytes allocated %d", limit, used)
	}
}

// The stored file is never larger than the upload, also when the EXIF record
// it came with is smaller than the usual orientation record.
func TestTheStoredFileIsNeverLargerThanTheUpload(t *testing.T) {
	small := string(smallTIFF(6))
	files := map[string][]byte{
		"image/jpeg": jpegWith(t, jpegSegment(0xE1, "Exif\x00\x00"+small)),
		"image/png":  pngWith(t, pngChunk("eXIf", small)),
		"image/webp": webpOf(webpExtended(webpFlagEXIF), webpChunk("VP8L", webpPixel), webpChunk("EXIF", small)),
	}
	for mimeType, in := range files {
		for _, buffer := range []int{wholeFile, noBuffer} {
			out := strip(t, mimeType, in, buffer)
			if len(out) > len(in) || shownOrientation(out) != 6 {
				t.Errorf("%s (buffer %d): %d bytes in, %d out, orientation %d", mimeType, buffer, len(in), len(out), shownOrientation(out))
			}
		}
	}
}

// A file that ends in the middle of a segment arrives as far as it goes,
// without the metadata it had.
func TestATruncatedFileEndsQuietly(t *testing.T) {
	files := map[string][]byte{"image/jpeg": phoneJPEG(t, 6), "image/png": phonePNG(t, 6), "image/webp": phoneWebP(6)}
	for mimeType, in := range files {
		for _, buffer := range []int{wholeFile, noBuffer} {
			for cut := range len(in) {
				out := strip(t, mimeType, in[:cut], buffer)
				if len(out) > cut {
					t.Fatalf("%s cut at %d grew to %d bytes", mimeType, cut, len(out))
				}
				if bytes.Contains(out, []byte(camMark)) {
					t.Fatalf("%s cut at %d keeps its EXIF record", mimeType, cut)
				}
			}
		}
	}
}
