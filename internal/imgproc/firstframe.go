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

// firstWebPFrame redraws the first frame of an animated WebP. The decoder
// cannot read the animation container, so the frame's own bitstream (an ANMF
// chunk holds it after a 16 byte header) is wrapped in a still WebP file.
func firstWebPFrame(data []byte, k kind) (*Picture, error) {
	still := stillFromAnimation(data)
	if still == nil {
		return nil, reject("the image cannot be read")
	}
	cfg, err := webp.DecodeConfig(bytes.NewReader(still))
	if err != nil {
		return nil, reject("the image cannot be read")
	}
	if err := checkCanvas(cfg, k); err != nil {
		return nil, err
	}
	src, err := webp.Decode(bytes.NewReader(still))
	if err != nil {
		return nil, reject("the image cannot be read")
	}
	return encode(k.cover(src), "image/png")
}

// stillFromAnimation returns a still WebP file made of the first ANMF frame, or
// nil when there is none.
func stillFromAnimation(data []byte) []byte {
	for pos := 12; pos+8 <= len(data); {
		size := int(binary.LittleEndian.Uint32(data[pos+4 : pos+8]))
		end := pos + 8 + size
		if size < 0 || end > len(data) {
			return nil
		}
		if string(data[pos:pos+4]) == "ANMF" && size > 16 {
			return wrapFrame(data[pos+8 : end])
		}
		pos = end + size%2 // chunks are padded to an even size
	}
	return nil
}

// wrapFrame builds RIFF/WEBP with an extended header (alpha allowed) around
// the sub-chunks of one ANMF payload: 16 bytes of frame header, then the
// ALPH/VP8/VP8L chunks.
func wrapFrame(anmf []byte) []byte {
	width := 1 + (int(anmf[6]) | int(anmf[7])<<8 | int(anmf[8])<<16)
	height := 1 + (int(anmf[9]) | int(anmf[10])<<8 | int(anmf[11])<<16)
	frame := anmf[16:]
	vp8x := make([]byte, 10)
	vp8x[0] = 0x10 // alpha
	w, h := width-1, height-1
	vp8x[4], vp8x[5], vp8x[6] = byte(w), byte(w>>8), byte(w>>16)
	vp8x[7], vp8x[8], vp8x[9] = byte(h), byte(h>>8), byte(h>>16)
	var out bytes.Buffer
	out.WriteString("RIFF")
	_ = binary.Write(&out, binary.LittleEndian, uint32(4+8+len(vp8x)+len(frame)))
	out.WriteString("WEBPVP8X")
	_ = binary.Write(&out, binary.LittleEndian, uint32(len(vp8x)))
	out.Write(vp8x)
	out.Write(frame)
	return out.Bytes()
}
