package imgproc

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
)

// withOrientation inserts an EXIF APP1 segment carrying the orientation tag
// (big endian) right after the SOI marker of a JPEG.
func withOrientation(t *testing.T, jpegData []byte, orientation int) []byte {
	t.Helper()
	var tiff bytes.Buffer
	tiff.WriteString("MM\x00\x2a")
	_ = binary.Write(&tiff, binary.BigEndian, uint32(8))
	_ = binary.Write(&tiff, binary.BigEndian, uint16(1))
	_ = binary.Write(&tiff, binary.BigEndian, uint16(0x0112))
	_ = binary.Write(&tiff, binary.BigEndian, uint16(3))
	_ = binary.Write(&tiff, binary.BigEndian, uint32(1))
	_ = binary.Write(&tiff, binary.BigEndian, uint16(orientation))
	tiff.Write([]byte{0, 0})
	_ = binary.Write(&tiff, binary.BigEndian, uint32(0))
	payload := append([]byte("Exif\x00\x00"), tiff.Bytes()...)
	var out bytes.Buffer
	out.Write(jpegData[:2])
	out.Write([]byte{0xFF, 0xE1})
	_ = binary.Write(&out, binary.BigEndian, uint16(len(payload)+2))
	out.Write(payload)
	out.Write(jpegData[2:])
	return out.Bytes()
}

// halves is a w x h picture, red on the left half and blue on the right.
func halves(w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			c := color.NRGBA{R: 255, A: 255}
			if x >= w/2 {
				c = color.NRGBA{B: 255, A: 255}
			}
			img.SetNRGBA(x, y, c)
		}
	}
	return img
}

func TestJPEGOrientationIsRead(t *testing.T) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, halves(16, 8), nil); err != nil {
		t.Fatal(err)
	}
	if got := jpegOrientation(buf.Bytes()); got != 1 {
		t.Fatalf("no EXIF: %d", got)
	}
	for _, o := range []int{1, 3, 6, 8} {
		if got := jpegOrientation(withOrientation(t, buf.Bytes(), o)); got != o {
			t.Errorf("orientation %d read as %d", o, got)
		}
	}
	if got := jpegOrientation(withOrientation(t, buf.Bytes(), 9)); got != 1 {
		t.Errorf("an invalid value must read as upright, got %d", got)
	}
	if got := jpegOrientation([]byte("\xff\xd8\xff\xe1\x00\x05Ex")); got != 1 {
		t.Errorf("a truncated segment: %d", got)
	}
}

func TestOrientTurnsPicturesUpright(t *testing.T) {
	// A 4x2 picture with one marked corner pixel at the top left.
	src := image.NewNRGBA(image.Rect(0, 0, 4, 2))
	mark := color.NRGBA{R: 255, A: 255}
	src.SetNRGBA(0, 0, mark)
	// Where the marked pixel lands for each orientation.
	want := map[int]image.Point{
		1: {0, 0}, 2: {3, 0}, 3: {3, 1}, 4: {0, 1},
		5: {0, 0}, 6: {1, 0}, 7: {1, 3}, 8: {0, 3},
	}
	for o, at := range want {
		got := orient(src, o)
		b := got.Bounds()
		if swapsAxes(o) != (b.Dx() == 2 && b.Dy() == 4) {
			t.Errorf("orientation %d: size %v", o, b)
		}
		if got.At(at.X, at.Y) != mark {
			t.Errorf("orientation %d: marked pixel is not at %v", o, at)
		}
	}
}

func TestAvatarFromAPhoneJPEGIsUpright(t *testing.T) {
	// Stored sideways: 200x100, red left and blue right. Orientation 6 means
	// "rotate a quarter clockwise to view", so upright it is red on top.
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, halves(200, 100), &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		orientation int
		topRed      bool
	}{{1, false}, {6, true}} {
		out, err := ProcessAvatar(withOrientation(t, buf.Bytes(), c.orientation))
		if err != nil {
			t.Fatal(err)
		}
		img, err := jpeg.Decode(bytes.NewReader(out.Data))
		if err != nil {
			t.Fatal(err)
		}
		r, _, b, _ := img.At(50, 5).RGBA()
		isRed := r>>8 > 150 && b>>8 < 100
		if isRed != c.topRed {
			t.Errorf("orientation %d: top of the avatar red = %v, want %v", c.orientation, isRed, c.topRed)
		}
	}
}
