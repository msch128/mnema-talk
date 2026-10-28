package imgproc

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/draw"
	"image/gif"

	"golang.org/x/image/webp"
)

// firstGIFFrame redraws the first frame of a GIF, whatever its length, as a
// still PNG. Only that frame is decoded.
func firstGIFFrame(data []byte, k kind) (*Picture, error) {
	cfg, err := gif.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, reject("the image cannot be read")
	}
	if err := checkCanvas(cfg, k); err != nil {
		return nil, err
	}
	frame, err := gif.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, reject("the image cannot be read")
	}
	canvas := image.NewNRGBA(image.Rect(0, 0, cfg.Width, cfg.Height))
	draw.Draw(canvas, frame.Bounds(), frame, frame.Bounds().Min, draw.Over)
	return encode(k.cover(canvas), "image/png")
}

// firstWebPFrame redraws the first frame of an animated WebP onto the
// animation's canvas, at the frame's offset. The decoder cannot read the
// animation container, so the frame's own bitstream (an ANMF chunk holds it
// after a 16 byte header) is wrapped in a still WebP file.
func firstWebPFrame(data []byte, k kind) (*Picture, error) {
	// The extended header (VP8X) holds the canvas size: flags, 3 reserved
	// bytes, then width-1 and height-1 as 24 bit little endian numbers.
	if len(data) < 30 {
		return nil, reject("the image cannot be read")
	}
	canvasCfg := image.Config{Width: 1 + le24(data[24:]), Height: 1 + le24(data[27:])}
	if err := checkCanvas(canvasCfg, k); err != nil {
		return nil, err
	}
	still, at := stillFromAnimation(data)
	if still == nil {
		return nil, reject("the image cannot be read")
	}
	cfg, err := webp.DecodeConfig(bytes.NewReader(still))
	if err != nil || at.X+cfg.Width > canvasCfg.Width || at.Y+cfg.Height > canvasCfg.Height {
		return nil, reject("the image cannot be read")
	}
	src, err := webp.Decode(bytes.NewReader(still))
	if err != nil {
		return nil, reject("the image cannot be read")
	}
	canvas := image.NewNRGBA(image.Rect(0, 0, canvasCfg.Width, canvasCfg.Height))
	draw.Draw(canvas, src.Bounds().Sub(src.Bounds().Min).Add(at), src, src.Bounds().Min, draw.Over)
	return encode(k.cover(canvas), "image/png")
}

func le24(b []byte) int { return int(b[0]) | int(b[1])<<8 | int(b[2])<<16 }

// stillFromAnimation returns a still WebP file made of the first ANMF frame
// and the frame's offset on the canvas, or nil when there is no frame.
func stillFromAnimation(data []byte) ([]byte, image.Point) {
	for pos := 12; pos+8 <= len(data); {
		size := int(binary.LittleEndian.Uint32(data[pos+4 : pos+8]))
		end := pos + 8 + size
		if end > len(data) {
			return nil, image.Point{}
		}
		if string(data[pos:pos+4]) == "ANMF" && size > 16 {
			anmf := data[pos+8 : end]
			// The offsets are stored in units of two pixels.
			return wrapFrame(anmf), image.Pt(2*le24(anmf), 2*le24(anmf[3:]))
		}
		pos = end + size%2 // chunks are padded to an even size
	}
	return nil, image.Point{}
}

// wrapFrame builds RIFF/WEBP with an extended header (alpha allowed) around
// the sub-chunks of one ANMF payload: 16 bytes of frame header, then the
// ALPH/VP8/VP8L chunks. The still's canvas is the frame itself.
func wrapFrame(anmf []byte) []byte {
	frame := anmf[16:]
	vp8x := make([]byte, 10)
	vp8x[0] = 0x10             // alpha
	copy(vp8x[4:], anmf[6:12]) // frame width-1 and height-1
	var out bytes.Buffer
	out.WriteString("RIFF")
	_ = binary.Write(&out, binary.LittleEndian, uint32(4+8+len(vp8x)+len(frame)))
	out.WriteString("WEBPVP8X")
	_ = binary.Write(&out, binary.LittleEndian, uint32(len(vp8x)))
	out.Write(vp8x)
	out.Write(frame)
	return out.Bytes()
}
