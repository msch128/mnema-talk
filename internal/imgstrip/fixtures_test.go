package imgstrip

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"

	"github.com/msch128/mnema-talk/internal/imgproc"
)

// Every piece of metadata in the fixtures carries one of these, so a test can
// tell that it is gone; the ICC profile's stays.
const (
	camMark  = "GPS-FIXTURE-CAM"
	xmpMark  = "xmp-fixture-location"
	iptcMark = "iptc-fixture-byline"
	textMark = "text-fixture-comment"
	iccMark  = "icc-fixture-profile"
	// thumbMark stands for a thumbnail, which is a small copy of the photo
	// and can carry an EXIF record of its own.
	thumbMark = "thumbnail-fixture-GPS"
)

var metadataMarks = []string{camMark, xmpMark, iptcMark, textMark, thumbMark}

const xmpPacket = `<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:Description exif:GPSLatitude="` + xmpMark + `"/></x:xmpmeta>`

// gpsTIFF is an EXIF record as a phone writes it, cut down: camera make,
// orientation and a GPS directory with a position.
func gpsTIFF(orientation int) []byte {
	be := binary.BigEndian
	entry := func(tag, kind uint16, count uint32, value []byte) []byte {
		e := be.AppendUint32(be.AppendUint16(be.AppendUint16(nil, tag), kind), count)
		return append(e, append(value, 0, 0, 0, 0)[:4]...)
	}
	const (
		ascii, short, long, rational = 2, 3, 4, 5
		makeAt, gpsAt, latAt, lonAt  = 50, 66, 120, 144
	)
	t := []byte("MM\x00\x2a\x00\x00\x00\x08\x00\x03")
	t = append(t, entry(0x010F, ascii, 16, be.AppendUint32(nil, makeAt))...)
	t = append(t, entry(0x0112, short, 1, []byte{0, byte(orientation)})...)
	t = append(t, entry(0x8825, long, 1, be.AppendUint32(nil, gpsAt))...)
	t = append(t, 0, 0, 0, 0)
	t = append(t, camMark+"\x00"...)
	t = append(t, 0, 4)
	t = append(t, entry(1, ascii, 2, []byte("N\x00"))...)
	t = append(t, entry(2, rational, 3, be.AppendUint32(nil, latAt))...)
	t = append(t, entry(3, ascii, 2, []byte("E\x00"))...)
	t = append(t, entry(4, rational, 3, be.AppendUint32(nil, lonAt))...)
	t = append(t, 0, 0, 0, 0)
	for _, v := range []uint32{12, 1, 34, 1, 56, 1, 123, 1, 45, 1, 6, 1} {
		t = be.AppendUint32(t, v)
	}
	return t
}

// picture is 8x4 with a different colour in every corner, so a turned or
// re-encoded copy does not compare equal.
func picture() image.Image {
	m := image.NewNRGBA(image.Rect(0, 0, 8, 4))
	for y := range 4 {
		for x := range 8 {
			m.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 30), G: uint8(y * 60), B: uint8(x*y*8 + 10), A: 255})
		}
	}
	return m
}

func jpegSegment(marker byte, payload string) []byte {
	return append([]byte{0xFF, marker, byte((len(payload) + 2) >> 8), byte(len(payload) + 2)}, payload...)
}

func plainJPEG(t testing.TB) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, picture(), &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// jpegWith puts segments between the start marker and the picture.
func jpegWith(t testing.TB, segments ...[]byte) []byte {
	t.Helper()
	plain := plainJPEG(t)
	return append(append([]byte{0xFF, 0xD8}, bytes.Join(segments, nil)...), plain[2:]...)
}

var (
	jfifSegment  = jpegSegment(0xE0, "JFIF\x00\x01\x01\x00\x00\x01\x00\x01\x00\x00")
	iccSegment   = jpegSegment(0xE2, "ICC_PROFILE\x00\x01\x01"+iccMark)
	adobeSegment = jpegSegment(0xEE, "Adobe\x00\x64\x00\x00\x00\x00\x01")
)

// smallTIFF is the shortest EXIF record an orientation can be read from: it
// has no offset of a next directory.
func smallTIFF(orientation int) []byte {
	return append([]byte("II\x2a\x00\x08\x00\x00\x00\x01\x00\x12\x01\x03\x00\x01\x00\x00\x00"), byte(orientation), 0, 0, 0)
}

// phoneJPEG is a photo as a phone sends it: position, camera, XMP, IPTC, a
// comment and a vendor block, next to what the picture needs.
func phoneJPEG(t testing.TB, orientation int) []byte {
	return jpegWith(t,
		jfifSegment,
		jpegSegment(0xE1, "Exif\x00\x00"+string(gpsTIFF(orientation))),
		jpegSegment(0xE1, "http://ns.adobe.com/xap/1.0/\x00"+xmpPacket),
		iccSegment,
		jpegSegment(0xED, "Photoshop 3.0\x008BIM"+iptcMark),
		jpegSegment(0xEB, "JP\x00\x01"+textMark),
		adobeSegment,
		jpegSegment(0xFE, textMark),
	)
}

func pngChunk(kind, data string) []byte {
	c := binary.BigEndian.AppendUint32(nil, uint32(len(data)))
	c = append(c, kind+data...)
	return binary.BigEndian.AppendUint32(c, crc32.ChecksumIEEE(c[4:]))
}

func plainPNG(t testing.TB) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, picture()); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// pngWith puts chunks between the header chunk and the picture.
func pngWith(t testing.TB, chunks ...[]byte) []byte {
	t.Helper()
	plain := plainPNG(t)
	const afterHeader = len(pngSignature) + 12 + 13
	return append(append(bytes.Clone(plain[:afterHeader]), bytes.Join(chunks, nil)...), plain[afterHeader:]...)
}

var (
	gammaChunk = pngChunk("gAMA", "\x00\x00\xb1\x8f")
	iccChunk   = pngChunk("iCCP", iccMark+"\x00\x00x")
)

func phonePNG(t testing.TB, orientation int) []byte {
	return pngWith(t,
		gammaChunk,
		iccChunk,
		pngChunk("eXIf", string(gpsTIFF(orientation))),
		pngChunk("tEXt", "Comment\x00"+textMark),
		pngChunk("iTXt", "XML:com.adobe.xmp\x00\x00\x00\x00\x00"+xmpPacket),
		pngChunk("zTXt", "Author\x00\x00"+iptcMark),
		pngChunk("tIME", "\x07\xea\x0b\x0a\x0c\x00\x00"),
	)
}

func webpChunk(kind, data string) []byte {
	c := binary.LittleEndian.AppendUint32([]byte(kind), uint32(len(data)))
	c = append(c, data...)
	if len(data)%2 == 1 {
		c = append(c, 0)
	}
	return c
}

func webpOf(chunks ...[]byte) []byte {
	body := bytes.Join(chunks, nil)
	return append(binary.LittleEndian.AppendUint32([]byte("RIFF"), uint32(len(body)+4)), append([]byte("WEBP"), body...)...)
}

// webpFrame is an animation frame of chunks.
func webpFrame(chunks ...[]byte) []byte {
	return webpChunk("ANMF", strings.Repeat("\x00", webpFrameHeader)+string(bytes.Join(chunks, nil)))
}

// webpPixel is the data of a lossless WebP of one pixel.
const webpPixel = "\x2f\x00\x00\x00\x10\x07\x10\x11\x11\x88\x88\xfe\x07\x00"

var (
	plainWebP    = webpOf(webpChunk("VP8L", webpPixel))
	webpICC      = webpChunk("ICCP", iccMark)
	webpExtended = func(flags byte) []byte { return webpChunk("VP8X", string([]byte{flags, 0, 0, 0, 0, 0, 0, 0, 0, 0})) }
)

func phoneWebP(orientation int) []byte {
	return webpOf(
		webpExtended(0x20|webpFlagEXIF|webpFlagXMP),
		webpICC,
		webpChunk("VP8L", webpPixel),
		webpChunk("EXIF", string(gpsTIFF(orientation))),
		webpChunk("XMP ", xmpPacket),
	)
}

// exifRecords lists the EXIF records a file carries, whatever its format.
func exifRecords(data []byte) [][]byte {
	var found [][]byte
	switch {
	case bytes.HasPrefix(data, []byte("\xff\xd8")):
		for pos := 2; pos+4 <= len(data) && data[pos] == 0xFF && data[pos+1] != 0xDA; {
			end := pos + 2 + int(binary.BigEndian.Uint16(data[pos+2:]))
			if data[pos+1] == jpegAPP1 && bytes.HasPrefix(data[pos+4:end], jpegExifHeader) {
				found = append(found, data[pos+10:end])
			}
			pos = end
		}
	case bytes.HasPrefix(data, []byte(pngSignature)):
		for pos := len(pngSignature); pos+8 <= len(data); {
			end := pos + 8 + int(binary.BigEndian.Uint32(data[pos:]))
			if string(data[pos+4:pos+8]) == "eXIf" {
				found = append(found, data[pos+8:end])
			}
			pos = end + 4
		}
	case isWebP(data):
		for pos := webpHeaderLen; pos+8 <= len(data); {
			size := int(binary.LittleEndian.Uint32(data[pos+4:]))
			if string(data[pos:pos+4]) == "EXIF" {
				found = append(found, data[pos+8:pos+8+size])
			}
			pos += 8 + size + size%2
		}
	}
	return found
}

// shownOrientation is how a viewer turns the file: by its first EXIF record.
func shownOrientation(data []byte) int {
	if records := exifRecords(data); len(records) > 0 {
		return imgproc.TIFFOrientation(records[0])
	}
	return 1
}
