package imgstrip

import (
	"encoding/binary"
	"hash/crc32"
)

// PNG: a signature, then chunks (a four-byte length, a four-letter type, the
// data, a checksum) up to IEND. Only the chunks that draw the picture stay:
// the four critical ones, transparency, the colour chunks and the animation
// chunks of an APNG. Every other chunk is dropped, whatever its name: text
// (tEXt, zTXt, and iTXt, which is where XMP lives), the modification time,
// content credentials, the private chunks of editors. EXIF (eXIf) is dropped
// too, but for its orientation. The file ends with IEND, or where a chunk
// states a length that cannot be.
const pngSignature = "\x89PNG\r\n\x1a\n"

var pngKeeps = map[string]bool{
	"IHDR": true, "PLTE": true, "IDAT": true, "IEND": true,
	"tRNS": true, "bKGD": true, "hIST": true, "pHYs": true, "sPLT": true, "sBIT": true,
	"cHRM": true, "gAMA": true, "iCCP": true, "sRGB": true, "cICP": true, "mDCV": true, "cLLI": true,
	"acTL": true, "fcTL": true, "fdAT": true,
}

func (f *filter) pngStart() error {
	b, err := f.take(len(pngSignature))
	if string(b) == pngSignature {
		f.step = f.pngChunk
	} else {
		f.step = f.passThrough
	}
	f.emit(b...)
	return err
}

func (f *filter) pngChunk() error {
	b, err := f.take(8)
	if len(b) < 8 {
		return err
	}
	size, kind := binary.BigEndian.Uint32(b[:4]), string(b[4:8])
	if size > 1<<31-1 {
		f.step = f.end
		return nil
	}
	f.left, f.mode, f.next, f.step = uint64(size)+4, keep, f.pngChunk, f.block
	switch {
	case kind == "eXIf":
		return f.pngExif(int(size))
	case !pngKeeps[kind]:
		f.mode = drop
		return nil
	case kind == "IEND":
		f.next = f.end
	}
	f.emit(b...)
	return nil
}

// pngExif drops an eXIf chunk and, for the first one that turns the picture,
// writes one with the orientation only.
func (f *filter) pngExif(size int) error {
	f.mode = drop
	n := min(size, captureLimit)
	data, err := f.take(n)
	f.left -= uint64(len(data))
	if len(data) < n {
		return err
	}
	if o := f.firstOrientation(data); turns(o) {
		chunk := append([]byte("eXIf"), orientationRecord(o, size)...)
		f.out = binary.BigEndian.AppendUint32(f.out, uint32(len(chunk)-4))
		f.emit(chunk...)
		f.out = binary.BigEndian.AppendUint32(f.out, crc32.ChecksumIEEE(chunk))
	}
	return err
}
