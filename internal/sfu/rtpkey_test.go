package sfu

import (
	"bytes"
	"testing"
)

// vp8Payload builds a VP8 RTP payload: descriptor with a 15-bit picture ID and
// TL0PICIDX (when pic16 is false, a 7-bit ID), then the frame tag and a body.
func vp8Payload(start, key bool, pic uint16, pic16 bool, tl0 uint8, body ...byte) []byte {
	first := byte(0x80) // X
	if start {
		first |= 0x10
	}
	out := []byte{first, 0xc0} // I and L
	if pic16 {
		out = append(out, 0x80|byte(pic>>8), byte(pic))
	} else {
		out = append(out, byte(pic&0x7f))
	}
	out = append(out, tl0)
	tag := byte(0x01)
	if key {
		tag = 0x00
	}
	return append(append(out, tag), body...)
}

func TestParseVP8(t *testing.T) {
	t.Run("15-bit keyframe start", func(t *testing.T) {
		d, ok := parseVP8(vp8Payload(true, true, 0x1234, true, 7, 1, 2))
		if !ok {
			t.Fatal("expected successful parse")
		}
		if !d.start || !d.key || !d.pic16 {
			t.Fatalf("unexpected frame flags: %+v", d)
		}
		if d.pic != 0x1234 || d.picPos != 2 || d.tl0 != 7 || d.tl0Pos != 4 {
			t.Fatalf("unexpected frame indices: %+v", d)
		}
	})

	t.Run("7-bit delta start", func(t *testing.T) {
		d, ok := parseVP8(vp8Payload(true, false, 0x55, false, 9, 1))
		if !ok {
			t.Fatal("expected successful parse")
		}
		if !d.start || d.key || d.pic16 {
			t.Fatalf("unexpected delta flags: %+v", d)
		}
		if d.pic != 0x55 || d.picPos != 2 || d.tl0Pos != 3 {
			t.Fatalf("unexpected delta indices: %+v", d)
		}
	})

	t.Run("continuation packet", func(t *testing.T) {
		d, ok := parseVP8(vp8Payload(false, true, 1, true, 1, 1))
		if !ok {
			t.Fatal("expected continuation to parse")
		}
		if d.start || d.key {
			t.Fatalf("continuation cannot be start or key: %+v", d)
		}
	})

	t.Run("minimal descriptor without extension", func(t *testing.T) {
		d, ok := parseVP8([]byte{0x10, 0x00, 0xaa})
		if !ok {
			t.Fatal("expected minimal descriptor to parse")
		}
		if !d.key {
			t.Fatal("expected keyframe")
		}
		if d.picPos != -1 || d.tl0Pos != -1 {
			t.Fatalf("unexpected positions: %+v", d)
		}
	})

	t.Run("later partition", func(t *testing.T) {
		d, ok := parseVP8([]byte{0x11, 0x00, 0xaa})
		if !ok {
			t.Fatal("expected partition to parse")
		}
		if d.start || d.key {
			t.Fatalf("partition 1 taken for a start: %+v", d)
		}
	})

	t.Run("tid and keyidx skipped", func(t *testing.T) {
		d, ok := parseVP8([]byte{0x90, 0x20, 0x00, 0x00, 0xaa})
		if !ok || !d.key {
			t.Fatalf("T byte not skipped: %+v ok=%v", d, ok)
		}
	})

	t.Run("invalid payloads", func(t *testing.T) {
		for name, p := range map[string][]byte{
			"empty":         {},
			"no extension":  {0x80},
			"no picture id": {0x80, 0x80},
			"short pic16":   {0x80, 0x80, 0x80},
			"no tl0":        {0x80, 0x40},
			"no body":       {0x10},
			"only tid":      {0x80, 0x20, 0x00},
		} {
			if _, ok := parseVP8(p); ok {
				t.Errorf("%s accepted", name)
			}
		}
	})
}

func TestPatchVP8(t *testing.T) {
	src := vp8Payload(true, true, 0x7ffe, true, 250, 9, 9)
	d, ok := parseVP8(src)
	if !ok {
		t.Fatalf("parse source: % x", src)
	}
	got := patchVP8(nil, src, d, 3, 10)
	nd, ok := parseVP8(got)
	if !ok {
		t.Fatalf("parse patched: % x", got)
	}
	if nd.pic != 1 || nd.tl0 != 4 || !nd.key {
		t.Fatalf("unexpected patched descriptors: %+v", nd)
	}
	if !bytes.Equal(got[5:], src[5:]) {
		t.Fatalf("payload body altered: got % x, want % x", got[5:], src[5:])
	}
	if got[2]&0x80 == 0 {
		t.Fatal("the M bit of a 15-bit picture ID was lost")
	}
	if src[3] != 0x7e && src[3] != 0xfe {
		// the source is untouched
		t.Fatalf("source modified: % x", src)
	}

	src = vp8Payload(true, false, 0x7f, false, 1, 5)
	d, ok = parseVP8(src)
	if !ok {
		t.Fatalf("parse delta source: % x", src)
	}
	got = patchVP8(got, src, d, 2, 0)
	nd, ok = parseVP8(got)
	if !ok {
		t.Fatalf("parse patched delta: % x", got)
	}
	if nd.pic != 1 || nd.pic16 || nd.tl0 != 1 {
		t.Fatalf("7-bit patch: %+v", nd)
	}

	// Nothing to patch: the packet comes back as it was.
	src = []byte{0x10, 0x00, 0xaa}
	d, ok = parseVP8(src)
	if !ok {
		t.Fatalf("parse plain source: % x", src)
	}
	if got = patchVP8(got, src, d, 5, 5); !bytes.Equal(got, src) {
		t.Fatalf("plain descriptor changed: % x", got)
	}
}

func TestH264Start(t *testing.T) {
	stap := func(nals ...[]byte) []byte {
		out := []byte{24}
		for _, n := range nals {
			out = append(out, byte(len(n)>>8), byte(len(n)))
			out = append(out, n...)
		}
		return out
	}
	sps, pps, idr, slice := []byte{0x67, 1, 2}, []byte{0x68, 3}, []byte{0x65, 4}, []byte{0x41, 5}
	for _, c := range []struct {
		name     string
		payload  []byte
		sps, idr bool
	}{
		{"single SPS", sps, true, false},
		{"single IDR", idr, false, true},
		{"single slice", slice, false, false},
		{"STAP-A with SPS", stap(sps, pps), true, false},
		{"STAP-A with IDR", stap(pps, idr), false, true},
		{"STAP-A with both", stap(sps, pps, idr), true, true},
		{"STAP-A of slices", stap(slice), false, false},
		{"STAP-A cut short", []byte{24, 0, 9, 0x67}, false, false},
		{"STAP-A empty unit", []byte{24, 0, 0, 0x67}, false, false},
		{"FU-A start of IDR", []byte{28, 0x85, 1}, false, true},
		{"FU-A middle of IDR", []byte{28, 0x05, 1}, false, false},
		{"FU-A start of slice", []byte{28, 0x81, 1}, false, false},
		{"FU-A header missing", []byte{28}, false, false},
		{"empty", nil, false, false},
	} {
		gotSPS, gotIDR := h264Start(c.payload)
		if gotSPS != c.sps || gotIDR != c.idr {
			t.Errorf("%s: sps=%v idr=%v, want sps=%v idr=%v", c.name, gotSPS, gotIDR, c.sps, c.idr)
		}
	}
}

func TestCodecKindOf(t *testing.T) {
	for mime, want := range map[string]codecKind{"video/VP8": codecVP8, "video/vp8": codecVP8, "video/H264": codecH264, "video/VP9": codecOther, "": codecOther} {
		if got := codecKindOf(mime); got != want {
			t.Errorf("%q: %v, want %v", mime, got, want)
		}
	}
}
