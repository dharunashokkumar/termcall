package video

// Auto-levels. Without this, ASCII video of a real room is close to unreadable.
//
// A webcam indoors puts almost everything it sees into a narrow band -- a
// measurement on a typical laptop camera found the whole picture inside
// luminance 125 to 140, out of 0 to 255. A ten step ramp maps that entire
// range onto one character, so a face arrives as a rectangle of plus signs.
// Stretching what is actually there across the full range is the difference
// between a picture and a texture.

// levels tracks where black and white sit, smoothed over time.
type levels struct {
	lo, hi float64
	ready  bool
}

const (
	// The stretch is anchored on percentiles, not the darkest and brightest
	// pixels: one blown-out highlight -- a lamp, a window, the white of an
	// eye -- would otherwise set white for the whole frame and flatten
	// everything else back down.
	loPct = 2
	hiPct = 98

	// A floor on the range being stretched. A blank wall has almost no
	// spread, and without this the gain climbs until sensor noise fills the
	// screen with crawling characters.
	minSpan = 48.0

	// New measurements are blended in slowly. Recomputing the levels per
	// frame makes the whole picture pulse whenever anything in shot moves,
	// which is far more distracting than being a few frames late to a
	// genuine change in lighting.
	blend = 0.15
)

// update folds this frame's spread into the running estimate.
func (l *levels) update(g *grid) {
	var hist [64]int
	n := 0
	for i := 0; i < len(g.px); i += 3 {
		hist[lum(g.px[i], g.px[i+1], g.px[i+2])>>2]++
		n++
	}
	if n == 0 {
		return
	}

	lo := float64(percentile(hist[:], n, loPct) << 2)
	hi := float64(percentile(hist[:], n, hiPct) << 2)
	if hi-lo < minSpan {
		// Widen around the middle of what is there, then slide the window
		// back inside 0..255 rather than letting it hang off the end. A flat
		// black frame centred at zero would otherwise stretch to a window
		// starting below black, which lifts the whole picture to grey -- an
		// unlit room would glow.
		mid := (hi + lo) / 2
		lo, hi = mid-minSpan/2, mid+minSpan/2
		switch {
		case lo < 0:
			lo, hi = 0, minSpan
		case hi > 255:
			lo, hi = 255-minSpan, 255
		}
	}

	if !l.ready {
		l.lo, l.hi, l.ready = lo, hi, true
		return
	}
	l.lo += (lo - l.lo) * blend
	l.hi += (hi - l.hi) * blend
}

// apply stretches the grid in place.
func (l *levels) apply(g *grid) {
	if !l.ready || l.hi <= l.lo {
		return
	}
	// One shared map across all three channels, rather than a per-channel
	// gain worked out from each pixel's brightness. A per-pixel gain is
	// unstable where the picture is darkest -- a pixel at luminance 1 wanting
	// to land at 100 needs a gain of 100, and its neighbour at luminance 2
	// needs 50, so shadows tear into confetti. One affine map is the standard
	// auto-levels, and it moves every pixel by a predictable amount.
	//
	// A lookup table beats the arithmetic: the same 256 inputs recur across
	// tens of thousands of subpixels.
	var lut [256]uint8
	scale := 255 / (l.hi - l.lo)
	for i := range lut {
		v := (float64(i) - l.lo) * scale
		switch {
		case v < 0:
			v = 0
		case v > 255:
			v = 255
		}
		lut[i] = uint8(v)
	}
	for i, v := range g.px {
		g.px[i] = lut[v]
	}
}

// percentile finds the bucket below which pct% of the samples fall.
func percentile(hist []int, n, pct int) int {
	want := n * pct / 100
	seen := 0
	for i, c := range hist {
		seen += c
		if seen >= want {
			return i
		}
	}
	return len(hist) - 1
}
