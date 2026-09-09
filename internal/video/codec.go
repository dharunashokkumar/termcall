package video

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
)

// Quality is the JPEG quality frames are sent at.
//
// 55 sits where the artefacts stop mattering. The picture is about to be
// reduced to a few thousand coloured characters, and block noise at that
// quality is finer than one terminal cell -- it disappears into the
// downscale. Raising it costs bandwidth to preserve detail the renderer is
// going to throw away regardless.
const Quality = 55

// Encoder turns frames into JPEGs, reusing its working memory. At ten frames
// a second for the length of a call, allocating a fresh image and buffer each
// time is garbage the collector has to chase for no reason.
type Encoder struct {
	rgba *image.RGBA
	buf  bytes.Buffer
}

// Encode returns the frame as a JPEG. The bytes belong to the Encoder and are
// overwritten by the next call, so a caller that keeps them must copy.
func (e *Encoder) Encode(f *Frame) ([]byte, error) {
	if f == nil {
		return nil, fmt.Errorf("no frame")
	}
	if e.rgba == nil || e.rgba.Rect.Dx() != f.W || e.rgba.Rect.Dy() != f.H {
		e.rgba = image.NewRGBA(image.Rect(0, 0, f.W, f.H))
	}
	// image/jpeg has a fast path for *image.RGBA and a slow per-pixel one for
	// anything else, so it is worth laying the frame out that way rather than
	// making Frame implement image.Image.
	for i, j := 0, 0; i < len(f.Pix); i, j = i+3, j+4 {
		e.rgba.Pix[j] = f.Pix[i]
		e.rgba.Pix[j+1] = f.Pix[i+1]
		e.rgba.Pix[j+2] = f.Pix[i+2]
		e.rgba.Pix[j+3] = 0xff
	}
	e.buf.Reset()
	if err := jpeg.Encode(&e.buf, e.rgba, &jpeg.Options{Quality: Quality}); err != nil {
		return nil, err
	}
	return e.buf.Bytes(), nil
}

// Decode turns a received JPEG back into a frame.
//
// The size is checked against the wire format rather than trusted: this data
// arrived from another machine, and a frame of unexpected dimensions would
// otherwise flow into the renderer's sampling arithmetic.
func Decode(b []byte) (*Frame, error) {
	img, err := jpeg.Decode(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	r := img.Bounds()
	w, h := r.Dx(), r.Dy()
	if w <= 0 || h <= 0 || w > 4*WireW || h > 4*WireH {
		return nil, fmt.Errorf("frame is %dx%d, which is not a picture we sent", w, h)
	}

	f := NewFrame(w, h)
	// Decoding a JPEG almost always yields YCbCr, and converting it in bulk
	// here beats the generic At() path by a wide margin at ten frames a
	// second times three peers.
	if yc, ok := img.(*image.YCbCr); ok {
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				c := yc.YCbCrAt(r.Min.X+x, r.Min.Y+y)
				rr, gg, bb := ycbcr(c.Y, c.Cb, c.Cr)
				i := (y*w + x) * 3
				f.Pix[i], f.Pix[i+1], f.Pix[i+2] = rr, gg, bb
			}
		}
		return f, nil
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			rr, gg, bb, _ := img.At(r.Min.X+x, r.Min.Y+y).RGBA()
			i := (y*w + x) * 3
			f.Pix[i], f.Pix[i+1], f.Pix[i+2] = uint8(rr>>8), uint8(gg>>8), uint8(bb>>8)
		}
	}
	return f, nil
}

// ycbcr is the JFIF conversion, in integer arithmetic. This is the same maths
// image/color performs; it is inlined here because it runs once per pixel per
// frame per peer, which is the hottest loop in a call.
func ycbcr(y, cb, cr uint8) (uint8, uint8, uint8) {
	yy := int32(y) * 0x10101
	cb1 := int32(cb) - 128
	cr1 := int32(cr) - 128

	r := yy + 91881*cr1
	g := yy - 22554*cb1 - 46802*cr1
	b := yy + 116130*cb1

	return clamp8(r), clamp8(g), clamp8(b)
}

func clamp8(v int32) uint8 {
	if uint32(v)&0xff000000 == 0 {
		return uint8(v >> 16)
	}
	if v < 0 {
		return 0
	}
	return 255
}
