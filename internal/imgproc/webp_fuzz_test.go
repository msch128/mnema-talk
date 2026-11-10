package imgproc

import (
	"bytes"
	"testing"
)

// FuzzIsAnimatedWebP checks the container sniffing of a WebP upload and the
// reader behind it that takes the first frame out of an animation: neither
// panics, and a picture that comes out is a PNG.
func FuzzIsAnimatedWebP(f *testing.F) {
	for _, seed := range [][]byte{
		animatedWebP(f), animatedWebPOn(f, 6, 6, 1, 2), animatedWebPOn(f, 3, 3, 2, 0),
		append([]byte("RIFF\x00\x00\x00\x00WEBPVP8X\x0a\x00\x00\x00\x02"), make([]byte, 20)...),
		append([]byte("RIFF\x16\x00\x00\x00WEBPVP8X\x0a\x00\x00\x00\x02"), make([]byte, 10)...),
		[]byte("RIFF\x1a\x00\x00\x00WEBPVP8X\x0a\x00\x00\x00\x12\x00\x00\x00\x00\x00\x00\x00\x00\x00"),
		[]byte("RIFF\x00\x00\x00\x00WEBPVP8X"), []byte("RIFF\x00\x00\x00\x00WEBP"), []byte("RIFF"), nil,
	} {
		f.Add(seed)
	}
	legacy := kind{minW: 1, minH: 1, cover: coverAvatar, legacy: true}
	f.Fuzz(func(t *testing.T, data []byte) {
		animated := IsAnimatedWebP(data)
		if animated && (len(data) < 21 || data[20]&0x02 == 0) {
			t.Fatalf("no animation flag in % x", data)
		}
		if !animated || !IsWebP(data) {
			return
		}
		if len(data) >= 30 && (le24(data[24:]) >= fuzzMaxEdge || le24(data[27:]) >= fuzzMaxEdge) {
			return
		}
		pic, err := firstWebPFrame(data, legacy)
		if err != nil {
			if pic != nil {
				t.Fatalf("a picture came back with the refusal %v", err)
			}
			return
		}
		if pic.ContentType != "image/png" || pic.Animated || !bytes.HasPrefix(pic.Data, []byte("\x89PNG\r\n\x1a\n")) {
			t.Fatalf("first frame came out as %s (animated %v)", pic.ContentType, pic.Animated)
		}
	})
}
