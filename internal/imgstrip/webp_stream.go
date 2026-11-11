package imgstrip

import "encoding/binary"

// A WebP larger than the buffer streams. Its length is in the header, which
// is gone by the time a chunk that has to go shows up, so nothing can be cut
// out. Such a chunk keeps its place and length instead and its data is
// overwritten: it becomes a RIFF "JUNK" chunk of zero bytes. Where the header
// announces EXIF, the flag is already written, so the first EXIF chunk stays
// one: it holds the orientation record and zero bytes. An EXIF chunk the
// header does not announce cannot be kept here (the flag cannot be set any
// more), so its orientation is lost. The chunks inside an animation frame
// are walked too (webpFrameChunk).

func (f *filter) webpHeader() error {
	b, err := f.take(webpHeaderLen)
	f.emit(b...)
	f.step = f.passThrough
	if isWebP(b) {
		f.container, f.first, f.step = uint64(binary.LittleEndian.Uint32(b[4:8]))-4, true, f.webpChunk
	}
	return err
}

func (f *filter) webpChunk() error {
	if f.container < 8 { // too little for a chunk
		f.left, f.mode, f.next, f.step = f.container, zero, f.end, f.block
		return nil
	}
	b, err := f.take(8)
	if len(b) < 8 {
		clear(b)
		f.emit(b...)
		return err
	}
	kind, size := string(b[:4]), uint64(binary.LittleEndian.Uint32(b[4:8]))
	f.container -= 8
	f.left, f.mode, f.next, f.step = size, keep, f.webpChunk, f.block
	switch {
	case size > f.container: // the container ends inside this chunk
		f.left, f.next = f.container, f.end
	case size&1 == 1 && f.container > size:
		f.left++
	}
	f.container -= f.left
	first := f.first
	f.first = false
	switch {
	case kind == "EXIF" && f.left >= size:
		return f.webpExif(size)
	case kind == "VP8X" && f.left > 0:
		f.emit(b...)
		return f.webpFlags(first)
	case kind == "ANMF":
		f.frame = min(size, f.left)
		f.framePad, f.afterFrame = f.left-f.frame, f.next
		f.left, f.next = min(webpFrameHeader, f.frame), f.webpFrameChunk
		f.frame -= f.left
	case !webpKeeps[kind]:
		f.mode = zero
		copy(b, "JUNK")
	}
	f.emit(b...)
	return nil
}

// webpFlags passes the first byte of an extended header on without the
// announcement of XMP. For the header in front it notes whether it announces
// EXIF; one further back, where no decoder reads it, announces nothing.
func (f *filter) webpFlags(first bool) error {
	b, err := f.take(1)
	if len(b) == 1 {
		f.left--
		if first {
			f.exifFlag = b[0]&webpFlagEXIF != 0
		} else {
			b[0] &^= webpFlagEXIF
		}
		f.emit(b[0] &^ webpFlagXMP)
	}
	return err
}

// webpFrameChunk is the state inside an animation frame, between its chunks
// (see webpBlankFrame, which does the same to a file in memory).
func (f *filter) webpFrameChunk() error {
	f.mode, f.next, f.step = zero, f.webpFrameChunk, f.block
	if f.frame < 8 { // the frame ends: its last bytes, then its pad byte
		f.left, f.frame, f.next = f.frame, 0, f.webpFramePad
		return nil
	}
	b, err := f.take(8)
	if len(b) < 8 {
		clear(b)
		f.emit(b...)
		return err
	}
	size := uint64(binary.LittleEndian.Uint32(b[4:8]))
	f.frame -= 8
	f.left = min(f.frame, size+size&1)
	f.frame -= f.left
	if webpFrameKeeps[string(b[:4])] {
		f.mode = keep
	} else {
		copy(b, "JUNK")
	}
	f.emit(b...)
	return nil
}

// webpFramePad passes the pad byte of a frame on, if it has one.
func (f *filter) webpFramePad() error {
	f.left, f.mode, f.next, f.step = f.framePad, keep, f.afterFrame, f.block
	return nil
}

// webpExif overwrites an EXIF chunk of size bytes. The first one of a file
// that announces EXIF stays an EXIF chunk: with the orientation where there
// is room for the record, else with no tag at all.
func (f *filter) webpExif(size uint64) error {
	f.mode = zero
	n := int(min(size, captureLimit))
	data, err := f.take(n)
	f.left -= uint64(len(data))
	kind, record := "JUNK", []byte(nil)
	if len(data) == n && f.exifFlag && !f.oriented {
		kind, record = "EXIF", webpRecord(f.firstOrientation(webpOrientationSource(data)), n)
	}
	clear(data)
	copy(data, record)
	f.out = append(f.out, kind...)
	f.out = binary.LittleEndian.AppendUint32(f.out, uint32(size))
	f.emit(data...)
	return err
}

// webpRecord is the EXIF record that fits into room bytes and says o.
func webpRecord(o, room int) []byte {
	switch {
	case room >= orientationMin:
		return orientationRecord(max(o, 1), room)
	case room >= len(emptyTIFF):
		return []byte(emptyTIFF)
	}
	return nil
}
