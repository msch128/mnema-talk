package imgstrip

import (
	"bytes"
)

// JPEG: segments (0xFF, a marker, a two-byte length that counts itself, the
// payload) and, after each start of scan, entropy-coded image data, up to the
// end marker. In the data a 0xFF is followed by 0x00 (a data byte) or a
// restart marker; anything else after 0xFF is the next segment. A
// progressive file has several scans with segments between them, so the walk
// goes through all of it; a 0xFFD9 inside a payload is skipped with its
// segment and never taken for the end.
//
// Of the application segments only three say how to draw the picture and
// stay, in the form a decoder reads: the JFIF header (APP0 "JFIF", without
// the thumbnail it may carry), the ICC profile (APP2 "ICC_PROFILE", every
// part of it) and Adobe's colour transform (APP14 "Adobe", its twelve
// bytes). Every other one is dropped: EXIF and XMP (APP1), JFXX thumbnails
// (APP0), multi-picture and FlashPix data (APP2), IPTC and Photoshop
// (APP13), content credentials (APP11), vendor blocks, and the comments.
// A segment under a marker that is neither an application segment nor one a
// decoder draws from (reserved markers, JPEG extensions) is dropped as well.
const (
	jpegSOI   = 0xD8
	jpegEOI   = 0xD9
	jpegSOS   = 0xDA
	jpegAPP0  = 0xE0
	jpegAPP1  = 0xE1
	jpegAPP2  = 0xE2
	jpegAPP14 = 0xEE
	jpegAPP15 = 0xEF
	jpegCOM   = 0xFE
	// jfifLen is a JFIF header up to its thumbnail size, adobeLen the Adobe
	// segment a decoder reads.
	jfifLen  = 14
	adobeLen = 12
)

var (
	jpegExifHeader = []byte("Exif\x00\x00")
	jfifHeader     = []byte("JFIF\x00")
	iccHeader      = []byte("ICC_PROFILE\x00")
	adobeHeader    = []byte("Adobe")
)

// jpegStandsAlone reports whether a marker has no length and payload: the
// stuffed zero, TEM, the restart markers and a stray start marker.
func jpegStandsAlone(marker byte) bool {
	return marker <= 0x01 || (marker >= 0xD0 && marker <= jpegSOI)
}

// jpegDraws reports whether a marker starts a segment a decoder draws the
// picture from: the frame headers (SOF, but for the reserved 0xC8), the
// Huffman, arithmetic and quantization tables, the scan header, the restart
// interval and the segments of the hierarchical mode and of a late line
// count. Reserved markers and the JPEG extensions are not among them.
func jpegDraws(marker byte) bool {
	return (marker >= 0xC0 && marker <= 0xCF && marker != 0xC8) || (marker >= jpegSOS && marker <= 0xDF)
}

// jpegIsMetadata reports whether a marker is an application segment or a
// comment: the ones that stay only in a recognised form.
func jpegIsMetadata(marker byte) bool {
	return marker == jpegCOM || (marker >= jpegAPP0 && marker <= jpegAPP15)
}

func (f *filter) jpegStart() error {
	b, err := f.take(2)
	if len(b) < 2 || b[0] != 0xFF || b[1] != jpegSOI {
		f.step = f.passThrough
	} else {
		f.step = f.jpegData
	}
	f.emit(b...)
	return err
}

// jpegData reads up to the next segment. After a start of scan the bytes are
// image data and are copied. Anywhere else a file has nothing but segments,
// so what a broken or hand-made file puts between them is left out, as a
// decoder skips it. Fill bytes (more 0xFF in front of a marker) are left out
// everywhere; they mean nothing and would be orphaned in front of a dropped
// segment.
func (f *filter) jpegData() error {
	for range blockSize {
		b, err := f.bytes.ReadByte()
		if err != nil {
			return err
		}
		switch {
		case !f.sawFF:
			if f.sawFF = b == 0xFF; !f.sawFF && f.inScan {
				f.emit(b)
			}
		case b == 0xFF:
		case b == jpegEOI:
			f.emit(0xFF, b)
			f.step = f.end
			return nil
		case jpegStandsAlone(b):
			if f.inScan {
				f.emit(0xFF, b)
			}
			f.sawFF = false
		default:
			f.sawFF = false
			return f.jpegSegment(b)
		}
	}
	return nil
}

// jpegSegment reads the length and payload of a segment and emits what stays
// of it. A file that ends inside a segment, or states a length that cannot
// be, ends there.
func (f *filter) jpegSegment(marker byte) error {
	b, err := f.take(2)
	if len(b) < 2 {
		return err
	}
	size := int(b[0])<<8 | int(b[1])
	if size < 2 {
		f.step = f.end
		return nil
	}
	payload, err := f.take(size - 2)
	whole := len(payload) == size-2
	f.inScan = marker == jpegSOS
	switch {
	case jpegDraws(marker):
		f.jpegEmit(marker, payload, size-2)
	case whole && jpegIsMetadata(marker):
		f.jpegMetadata(marker, payload)
	}
	return err
}

// jpegEmit writes a segment that states a payload of size bytes.
func (f *filter) jpegEmit(marker byte, payload []byte, size int) {
	f.emit(0xFF, marker, byte((size+2)>>8), byte(size+2))
	f.emit(payload...)
}

// jpegMetadata emits the part of an application segment that draws the
// picture, if it has one.
func (f *filter) jpegMetadata(marker byte, payload []byte) {
	switch {
	case marker == jpegAPP0 && bytes.HasPrefix(payload, jfifHeader):
		if len(payload) >= jfifLen { // version, density, and a thumbnail of 0x0
			payload = append(payload[:jfifLen-2], 0, 0)
		}
		f.jpegEmit(marker, payload, len(payload))
	case marker == jpegAPP2 && bytes.HasPrefix(payload, iccHeader):
		f.jpegEmit(marker, payload, len(payload))
	case marker == jpegAPP14 && len(payload) >= adobeLen && bytes.HasPrefix(payload, adobeHeader):
		f.jpegEmit(marker, payload[:adobeLen], adobeLen)
	case marker == jpegAPP1 && bytes.HasPrefix(payload, jpegExifHeader):
		tiff := payload[len(jpegExifHeader):]
		if o := f.firstOrientation(tiff); turns(o) {
			record := orientationRecord(o, len(tiff))
			f.emit(0xFF, jpegAPP1, 0, byte(2+len(jpegExifHeader)+len(record)))
			f.emit(jpegExifHeader...)
			f.emit(record...)
		}
	}
}
