package sfu

import (
	"encoding/binary"
	"strings"
)

// The SFU never decodes video, but a viewer can only change from one layer of
// a screen share to another at a keyframe, and the sequence of picture IDs it
// sees must stay continuous. Both are read from the RTP payload descriptor,
// which the browser's media transform leaves readable (it seals only what the
// network path does not parse; see web/src/lib/media/codec.ts).

// codecKind is how a layer's keyframes are recognised.
type codecKind uint8

const (
	// codecOther is a codec the SFU cannot tell keyframes of. Such a share is
	// forwarded as one layer, as before layers existed.
	codecOther codecKind = iota
	codecVP8
	codecH264
)

func codecKindOf(mime string) codecKind {
	switch {
	case strings.EqualFold(mime, "video/vp8"):
		return codecVP8
	case strings.EqualFold(mime, "video/h264"):
		return codecH264
	}
	return codecOther
}

// vp8Descriptor is what the SFU reads of a VP8 payload descriptor (RFC 7741).
type vp8Descriptor struct {
	// start is the first packet of a frame: S set and partition 0.
	start bool
	// key is set on the first packet of a keyframe.
	key bool
	// picPos is the offset of the picture ID, -1 when absent. pic16 tells a
	// 15-bit ID (two bytes) from a 7-bit one.
	picPos int
	pic16  bool
	pic    uint16
	// tl0Pos is the offset of TL0PICIDX, -1 when absent.
	tl0Pos int
	tl0    uint8
}

// parseVP8 reads the descriptor at the start of payload. False when the
// payload is too short for the fields it announces.
func parseVP8(payload []byte) (vp8Descriptor, bool) {
	d := vp8Descriptor{picPos: -1, tl0Pos: -1}
	if len(payload) < 1 {
		return d, false
	}
	first := payload[0]
	at := 1
	if first&0x80 != 0 { // X: extension byte follows
		var ok bool
		if at, ok = d.parseExtension(payload); !ok {
			return d, false
		}
	}
	if len(payload) <= at {
		return d, false
	}
	d.start = first&0x10 != 0 && first&0x07 == 0
	// The first payload byte is the frame tag; bit 0 clear is a keyframe.
	d.key = d.start && payload[at]&0x01 == 0
	return d, true
}

// parseExtension reads the optional fields announced by the extension byte
// payload[1] into d and returns the offset of the first byte after them.
// False when the payload is too short.
func (d *vp8Descriptor) parseExtension(payload []byte) (at int, ok bool) {
	if len(payload) < 2 {
		return 0, false
	}
	ext := payload[1]
	at = 2
	if ext&0x80 != 0 { // I: picture ID
		if len(payload) <= at {
			return 0, false
		}
		d.picPos = at
		if payload[at]&0x80 != 0 {
			if len(payload) < at+2 {
				return 0, false
			}
			d.pic16 = true
			d.pic = binary.BigEndian.Uint16(payload[at:]) & 0x7fff
			at += 2
		} else {
			d.pic = uint16(payload[at])
			at++
		}
	}
	if ext&0x40 != 0 { // L: TL0PICIDX
		if len(payload) <= at {
			return 0, false
		}
		d.tl0Pos = at
		d.tl0 = payload[at]
		at++
	}
	if ext&0x30 != 0 { // T or K: TID/Y/KEYIDX byte
		at++
	}
	return at, true
}

// h264Start reports what an H.264 payload (RFC 6184) starts: sps when it
// carries a sequence parameter set (single, or inside a STAP-A), idr when it
// starts an IDR slice.
func h264Start(payload []byte) (sps, idr bool) {
	if len(payload) < 1 {
		return false, false
	}
	switch kind := payload[0] & 0x1f; {
	case kind == 7:
		return true, false
	case kind == 5:
		return false, true
	case kind == 24: // STAP-A: 16-bit sizes, then the NAL units
		at := 1
		for at+3 <= len(payload) {
			size := int(binary.BigEndian.Uint16(payload[at:]))
			at += 2
			if size == 0 || at+size > len(payload) {
				break
			}
			switch payload[at] & 0x1f {
			case 7:
				sps = true
			case 5:
				idr = true
			}
			at += size
		}
	case kind == 28: // FU-A: the start bit and the NAL type of the fragment
		if len(payload) >= 2 && payload[1]&0x80 != 0 && payload[1]&0x1f == 5 {
			idr = true
		}
	}
	return sps, idr
}

// patchVP8 returns payload with the picture ID moved by pic and TL0PICIDX by
// tl0, in buf (grown as needed; the returned slice aliases it). Fields the
// payload does not carry are left alone.
func patchVP8(buf, payload []byte, d vp8Descriptor, pic uint16, tl0 uint8) []byte {
	buf = append(buf[:0], payload...)
	if d.picPos >= 0 {
		if d.pic16 {
			v := (d.pic + pic) & 0x7fff
			binary.BigEndian.PutUint16(buf[d.picPos:], 0x8000|v)
		} else {
			buf[d.picPos] = byte((d.pic + pic) & 0x7f)
		}
	}
	if d.tl0Pos >= 0 {
		buf[d.tl0Pos] += tl0
	}
	return buf
}
