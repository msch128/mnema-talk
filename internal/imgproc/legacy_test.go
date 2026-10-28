package imgproc

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"image"
	"testing"
)

func TestLegacyAvatarTakesTheFirstFrameOfALongAnimation(t *testing.T) {
	long := gifOf(t, 300, 200, 150) // over the frame limit of new uploads
	if _, err := ProcessAvatar(long); err == nil {
		t.Fatal("new uploads keep the frame limit")
	}
	out, err := ProcessLegacyAvatar(long)
	if err != nil || out.Animated || out.ContentType != "image/png" || sizeOf(t, out.Data) != image.Pt(200, 200) {
		t.Fatalf("%v %v", out, err)
	}
}

func TestLegacyAvatarAllowsALargerCanvas(t *testing.T) {
	wide := pngOf(t, 8200, 40)
	if _, err := ProcessAvatar(wide); err == nil {
		t.Fatal("over today's edge limit")
	}
	out, err := ProcessLegacyAvatar(wide)
	if err != nil || sizeOf(t, out.Data) != image.Pt(40, 40) {
		t.Fatalf("%v %v", out, err)
	}
	if _, err := ProcessLegacyAvatar(pngOf(t, 31000, 2)); err == nil {
		t.Fatal("even legacy files have a limit")
	}
	if _, err := ProcessLegacyAvatar([]byte("GIF89a\x01")); err == nil {
		t.Fatal("a broken GIF")
	}
}

// animatedWebP wraps the one frame of a 1x1 lossless WebP in an animation.
func animatedWebP(t *testing.T) []byte {
	t.Helper()
	return animatedWebPOn(t, 1, 1, 0, 0)
}

// animatedWebPOn is animatedWebP on a canvas of canvasW x canvasH pixels, with
// the 1x1 frame at (2*x2, 2*y2).
func animatedWebPOn(t *testing.T, canvasW, canvasH, x2, y2 int) []byte {
	t.Helper()
	still, err := base64.StdEncoding.DecodeString("UklGRhoAAABXRUJQVlA4TA0AAAAvAAAAEAcQERGIiP4HAA==")
	if err != nil {
		t.Fatal(err)
	}
	frame := still[12:]
	anmf := make([]byte, 16, 16+len(frame))
	anmf[0], anmf[3] = byte(x2), byte(y2)
	anmf = append(anmf, frame...) // 1x1: the width and height fields stay 0 (= 1)
	var body bytes.Buffer
	body.WriteString("WEBPVP8X")
	_ = binary.Write(&body, binary.LittleEndian, uint32(10))
	body.Write([]byte{0x02, 0, 0, 0, byte(canvasW - 1), 0, 0, byte(canvasH - 1), 0, 0}) // animation flag, canvas size
	body.WriteString("ANMF")
	_ = binary.Write(&body, binary.LittleEndian, uint32(len(anmf)))
	body.Write(anmf)
	if len(anmf)%2 == 1 {
		body.WriteByte(0)
	}
	var out bytes.Buffer
	out.WriteString("RIFF")
	_ = binary.Write(&out, binary.LittleEndian, uint32(body.Len()))
	out.Write(body.Bytes())
	return out.Bytes()
}

func TestLegacyAvatarTakesTheFirstFrameOfAnAnimatedWebP(t *testing.T) {
	data := animatedWebP(t)
	if _, err := ProcessAvatar(data); err == nil {
		t.Fatal("new uploads refuse animated WebP")
	}
	out, err := ProcessLegacyAvatar(data)
	if err != nil || out.ContentType != "image/png" || sizeOf(t, out.Data) != image.Pt(1, 1) {
		t.Fatalf("%v %v", out, err)
	}
	// No frame at all, or a cut-off one.
	if _, err := ProcessLegacyAvatar(data[:30]); err == nil {
		t.Fatal("a truncated animation")
	}
	empty := append([]byte("RIFF\x16\x00\x00\x00WEBPVP8X\x0a\x00\x00\x00\x02"), make([]byte, 10)...)
	if _, err := ProcessLegacyAvatar(empty); err == nil {
		t.Fatal("an animation without a frame")
	}
}

func TestLegacyAvatarKeepsTheCanvasAndOffsetOfAnimatedWebP(t *testing.T) {
	// A 6x6 canvas whose first frame is a single pixel at (2,4): the avatar
	// is the whole canvas, not the 1x1 frame rectangle.
	out, err := ProcessLegacyAvatar(animatedWebPOn(t, 6, 6, 1, 2))
	if err != nil || sizeOf(t, out.Data) != image.Pt(6, 6) {
		t.Fatalf("%v %v", out, err)
	}
	// A frame that does not fit on its canvas is refused.
	if _, err := ProcessLegacyAvatar(animatedWebPOn(t, 3, 3, 2, 0)); err == nil {
		t.Fatal("frame outside the canvas")
	}
}
