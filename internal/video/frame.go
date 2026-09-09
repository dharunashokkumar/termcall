// Package video turns webcam pictures into things a terminal can draw.
//
// What travels between people is not ASCII. It is a small JPEG, ~192x144,
// and each terminal renders it locally. That costs a little more to send than
// shipping the finished characters would -- but only a little, because ANSI
// colour codes are bulky enough that a screen of them compresses no better
// than a photograph of the same scene. What it buys is that the person
// watching chooses the look and the size: they can switch render mode
// mid-call, and every tile redraws at whatever their terminal happens to be,
// rather than at whatever the sender guessed.
package video

// Frame is a decoded picture: 8-bit RGB, three bytes per pixel, row-major.
type Frame struct {
	W, H int
	Pix  []byte
}

func NewFrame(w, h int) *Frame {
	return &Frame{W: w, H: h, Pix: make([]byte, w*h*3)}
}

// Wire is the size frames are sent at. 4:3, because that is what webcams
// hand back and re-cropping it costs detail for nothing, and small enough
// that one JPEG is a couple of kilobytes -- at 10 frames a second that is
// ~25 KB/s to each peer, which three-way peer-to-peer can carry on a
// domestic uplink.
const (
	WireW = 192
	WireH = 144
)

// box averages the source rectangle [x0,x1) x [y0,y1). Averaging rather than
// picking the nearest pixel matters here: a 192-wide frame landing in a
// 40-column tile throws away four fifths of its pixels, and choosing one at
// random from each cell is what makes downscaled video crawl with noise.
func (f *Frame) box(x0, y0, x1, y1 int) (uint8, uint8, uint8) {
	if x1 <= x0 {
		x1 = x0 + 1
	}
	if y1 <= y0 {
		y1 = y0 + 1
	}
	x0, y0 = clamp(x0, 0, f.W-1), clamp(y0, 0, f.H-1)
	x1, y1 = clamp(x1, 1, f.W), clamp(y1, 1, f.H)

	var r, g, b, n int
	for y := y0; y < y1; y++ {
		row := y * f.W * 3
		for x := x0; x < x1; x++ {
			i := row + x*3
			r += int(f.Pix[i])
			g += int(f.Pix[i+1])
			b += int(f.Pix[i+2])
			n++
		}
	}
	if n == 0 {
		return 0, 0, 0
	}
	return uint8(r / n), uint8(g / n), uint8(b / n)
}

// lum is perceived brightness on the usual Rec. 601 weights. Everything that
// picks a character by "how bright is this" goes through here, so the three
// render modes agree about what dark means.
func lum(r, g, b uint8) int {
	return (299*int(r) + 587*int(g) + 114*int(b)) / 1000
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
