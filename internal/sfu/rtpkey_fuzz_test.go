package sfu

import (
	"bytes"
	"testing"
)

// vp8Seeds are the payloads of the descriptor unit tests.
func vp8Seeds() [][]byte {
	return [][]byte{
		vp8Payload(true, true, 0x1234, true, 7, 1, 2),
		vp8Payload(true, false, 0x55, false, 9, 1),
		vp8Payload(false, true, 1, true, 1, 1),
		vp8Payload(true, true, 0x7ffe, true, 250, 9, 9),
		vp8Payload(true, false, 0x7f, false, 1, 5),
		{0x10, 0x00, 0xaa},
		{0x11, 0x00, 0xaa},
		{0x90, 0x20, 0x00, 0x00, 0xaa},
		{},
		{0x80},
		{0x80, 0x80},
		{0x80, 0x80, 0x80},
		{0x80, 0x40},
		{0x10},
		{0x80, 0x20, 0x00},
	}
}

// FuzzParseVP8 checks that the descriptor reader never panics and that every
// offset it reports for an accepted payload lies inside that payload.
func FuzzParseVP8(f *testing.F) {
	for _, p := range vp8Seeds() {
		f.Add(p)
	}
	f.Fuzz(func(t *testing.T, payload []byte) {
		d, ok := parseVP8(payload)
		if !ok {
			return
		}
		if d.picPos >= len(payload) || (d.pic16 && d.picPos+1 >= len(payload)) {
			t.Fatalf("picture ID at %d (16 bit: %v) outside % x", d.picPos, d.pic16, payload)
		}
		if d.picPos < 0 && (d.pic16 || d.pic != 0) {
			t.Fatalf("picture ID without a position: %+v", d)
		}
		if d.pic > 0x7fff || (!d.pic16 && d.pic > 0x7f) {
			t.Fatalf("picture ID %#x wider than its field", d.pic)
		}
		if d.tl0Pos >= len(payload) {
			t.Fatalf("TL0PICIDX at %d outside % x", d.tl0Pos, payload)
		}
		if d.tl0Pos >= 0 && d.tl0Pos <= d.picPos {
			t.Fatalf("TL0PICIDX at %d before the picture ID at %d", d.tl0Pos, d.picPos)
		}
		if d.key && !d.start {
			t.Fatalf("keyframe that starts no frame: %+v", d)
		}
	})
}

// FuzzPatchVP8 checks the rewrite of an accepted payload: the copy has the
// length of the source, differs from it only in the picture ID and TL0PICIDX
// fields, reads back as the moved numbers, and the source stays untouched.
func FuzzPatchVP8(f *testing.F) {
	for _, p := range vp8Seeds() {
		f.Add(p, uint16(3), uint8(10))
		f.Add(p, uint16(0xffff), uint8(0xff))
	}
	f.Fuzz(func(t *testing.T, payload []byte, pic uint16, tl0 uint8) {
		d, ok := parseVP8(payload)
		if !ok {
			return
		}
		src := bytes.Clone(payload)
		got := patchVP8(make([]byte, 0, 4), payload, d, pic, tl0)
		if !bytes.Equal(payload, src) {
			t.Fatalf("source modified: % x, was % x", payload, src)
		}
		if len(got) != len(payload) {
			t.Fatalf("patched length %d, source %d", len(got), len(payload))
		}
		nd, ok := parseVP8(got)
		if !ok {
			t.Fatalf("patched payload no longer parses: % x", got)
		}
		mask := uint16(0x7f)
		if d.pic16 {
			mask = 0x7fff
		}
		want := d
		if d.picPos >= 0 {
			want.pic = (d.pic + pic) & mask
		}
		if d.tl0Pos >= 0 {
			want.tl0 = d.tl0 + tl0
		}
		if nd != want {
			t.Fatalf("patched descriptor %+v, want %+v", nd, want)
		}
		for i := range got {
			inPic := d.picPos >= 0 && (i == d.picPos || (d.pic16 && i == d.picPos+1))
			if !inPic && i != d.tl0Pos && got[i] != payload[i] {
				t.Fatalf("byte %d changed outside the patched fields: % x, source % x", i, got, payload)
			}
		}
	})
}

// FuzzH264Start checks the keyframe detection of H.264 payloads: it never
// panics, the single NAL unit types answer by their type alone, and what a
// truncated packet shows is also shown by the whole one.
func FuzzH264Start(f *testing.F) {
	for _, p := range [][]byte{
		{0x67, 1, 2},
		{0x65, 4},
		{0x41, 5},
		{24, 0, 3, 0x67, 1, 2, 0, 2, 0x68, 3},
		{24, 0, 2, 0x68, 3, 0, 2, 0x65, 4},
		{24, 0, 3, 0x67, 1, 2, 0, 2, 0x68, 3, 0, 2, 0x65, 4},
		{24, 0, 2, 0x41, 5},
		{24, 0, 9, 0x67},
		{24, 0, 0, 0x67},
		{28, 0x85, 1},
		{28, 0x05, 1},
		{28, 0x81, 1},
		{28},
		{},
	} {
		f.Add(p, uint16(len(p)/2))
	}
	f.Fuzz(func(t *testing.T, payload []byte, cut uint16) {
		sps, idr := h264Start(payload)
		if len(payload) == 0 {
			if sps || idr {
				t.Fatal("an empty payload starts something")
			}
			return
		}
		switch typ := payload[0] & 0x1f; typ {
		case 7:
			if !sps || idr {
				t.Fatalf("SPS read as sps=%v idr=%v", sps, idr)
			}
		case 5:
			if sps || !idr {
				t.Fatalf("IDR read as sps=%v idr=%v", sps, idr)
			}
		case 24:
		case 28:
			if sps {
				t.Fatal("a fragment read as a parameter set")
			}
		default:
			if sps || idr {
				t.Fatalf("NAL type %d read as sps=%v idr=%v", typ, sps, idr)
			}
		}
		headSPS, headIDR := h264Start(payload[:int(cut)%(len(payload)+1)])
		if (headSPS && !sps) || (headIDR && !idr) {
			t.Fatalf("the first %d bytes show sps=%v idr=%v, all of % x only sps=%v idr=%v",
				int(cut)%(len(payload)+1), headSPS, headIDR, payload, sps, idr)
		}
	})
}
