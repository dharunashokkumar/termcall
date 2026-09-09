package video

import (
	"strings"
	"testing"
)

// solid builds a frame from a function of position, so a test can describe a
// picture rather than spell one out.
func solid(w, h int, f func(x, y int) (uint8, uint8, uint8)) *Frame {
	fr := NewFrame(w, h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r, g, b := f(x, y)
			i := (y*w + x) * 3
			fr.Pix[i], fr.Pix[i+1], fr.Pix[i+2] = r, g, b
		}
	}
	return fr
}

func cells(s string) []rune {
	var out []rune
	esc := false
	for _, r := range s {
		switch {
		case esc:
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				esc = false
			}
		case r == 0x1b:
			esc = true
		default:
			out = append(out, r)
		}
	}
	return out
}

// Every mode must fill the grid it was asked for exactly. A row that is one
// cell short or long shifts everything drawn beside it in a tiled call.
func TestRenderFillsTheGrid(t *testing.T) {
	f := solid(WireW, WireH, func(x, y int) (uint8, uint8, uint8) {
		return uint8(x), uint8(y), 128
	})
	for _, m := range []Mode{Blocks, ASCII, Braille} {
		for _, size := range [][2]int{{40, 12}, {1, 1}, {97, 31}} {
			cols, rows := size[0], size[1]
			got := Render(f, m, cols, rows)
			if len(got) != rows {
				t.Fatalf("%v %dx%d: got %d rows", m, cols, rows, len(got))
			}
			for y, line := range got {
				if n := len(cells(line)); n != cols {
					t.Errorf("%v %dx%d: row %d has %d cells", m, cols, rows, y, n)
				}
				if !strings.HasSuffix(line, "\x1b[0m") {
					t.Errorf("%v: row %d does not end reset, colour will leak", m, y)
				}
			}
		}
	}
}

// Brightness must reach both ends of the ramp. A renderer that never emits a
// space or never reaches @ has lost half its contrast.
func TestASCIIUsesTheWholeRamp(t *testing.T) {
	f := solid(WireW, WireH, func(x, y int) (uint8, uint8, uint8) {
		v := uint8(x * 255 / WireW)
		return v, v, v
	})
	got := strings.Join(Render(f, ASCII, 60, 20), "")
	for _, want := range []rune{' ', '@'} {
		if !strings.ContainsRune(string(cells(got)), want) {
			t.Errorf("a black-to-white gradient never produced %q", want)
		}
	}
}

// Black must render as blank in every mode. If a dark room comes out as a
// field of characters, the picture is noise.
func TestBlackIsEmpty(t *testing.T) {
	f := solid(WireW, WireH, func(x, y int) (uint8, uint8, uint8) { return 0, 0, 0 })
	for _, m := range []Mode{ASCII, Braille} {
		for _, line := range Render(f, m, 30, 10) {
			if got := strings.TrimSpace(string(cells(line))); got != "" {
				t.Errorf("%v rendered black as %q", m, got)
			}
		}
	}
}

// Braille exists for edges. A hard vertical line has to survive into the
// output as lit dots, or the mode is not earning its place.
func TestBrailleFindsAnEdge(t *testing.T) {
	f := solid(WireW, WireH, func(x, y int) (uint8, uint8, uint8) {
		if x < WireW/2 {
			return 0, 0, 0
		}
		return 255, 255, 255
	})
	lit := 0
	for _, line := range Render(f, Braille, 40, 12) {
		for _, r := range cells(line) {
			if r >= 0x2800 && r <= 0x28ff && r != 0x2800 {
				lit++
			}
		}
	}
	if lit == 0 {
		t.Fatal("a black/white edge produced no braille dots at all")
	}
}

// Blocks is two colours in one cell; a picture that is dark on top and light
// below must come back as one row of ▀ carrying both.
func TestBlocksCarryTwoColours(t *testing.T) {
	f := solid(WireW, WireH, func(x, y int) (uint8, uint8, uint8) {
		if y < WireH/2 {
			return 0, 0, 0
		}
		return 255, 255, 255
	})
	rows := Render(f, Blocks, 20, 2)
	if len(rows) != 2 {
		t.Fatalf("got %d rows", len(rows))
	}
	for _, r := range cells(rows[0]) {
		if r != '▀' {
			t.Fatalf("blocks mode drew %q, want ▀", r)
		}
	}
	// The top row is entirely in the dark half, so both its colours are black;
	// the bottom row is entirely in the light half.
	if !strings.Contains(rows[0], "38;2;0;0;0") {
		t.Error("the dark half did not produce a black foreground")
	}
	if !strings.Contains(rows[1], "255;255;255") {
		t.Error("the light half did not produce white")
	}
}

// Fit is what stops faces being stretched. A terminal cell is twice as tall
// as it is wide, so a 4:3 picture wants about 2.7 columns per row.
func TestFitKeepsShape(t *testing.T) {
	cases := []struct{ cols, rows, srcW, srcH int }{
		{200, 50, 192, 144},
		{40, 40, 192, 144},
		{80, 24, 640, 480},
		{300, 10, 16, 9},
	}
	for _, c := range cases {
		w, h := Fit(c.cols, c.rows, c.srcW, c.srcH)
		if w > c.cols || h > c.rows {
			t.Errorf("Fit(%v) = %dx%d, which does not fit", c, w, h)
		}
		if w == 0 || h == 0 {
			t.Errorf("Fit(%v) collapsed to %dx%d", c, w, h)
			continue
		}
		// On screen a cell is 1 wide and 2 tall, so compare w : 2h.
		want := float64(c.srcW) / float64(c.srcH)
		got := float64(w) / (2 * float64(h))
		if got < want*0.8 || got > want*1.25 {
			t.Errorf("Fit(%v) = %dx%d, aspect %.2f, want ~%.2f", c, w, h, got, want)
		}
	}
}

// The measurement that motivated auto-levels: a real laptop webcam indoors
// put its entire picture between luminance 125 and 140. Without a stretch the
// ten-step ramp maps all of that onto one character, and a face renders as a
// solid rectangle.
func TestLowContrastIsStretched(t *testing.T) {
	f := solid(WireW, WireH, func(x, y int) (uint8, uint8, uint8) {
		v := uint8(125 + x*15/WireW)
		return v, v, v
	})

	flat := map[rune]bool{}
	for _, line := range Render(f, ASCII, 60, 20) {
		for _, r := range cells(line) {
			flat[r] = true
		}
	}
	if len(flat) < 4 {
		got := ""
		for r := range flat {
			got += string(r)
		}
		t.Errorf("a 125-140 picture rendered with %d distinct characters (%q); "+
			"auto-levels is not stretching it", len(flat), got)
	}
}

// Auto-levels must not invent light. An unlit room has to stay unlit, or the
// stretch turns sensor noise into a screenful of crawling characters.
func TestFlatBlackIsNotLifted(t *testing.T) {
	f := solid(WireW, WireH, func(x, y int) (uint8, uint8, uint8) { return 0, 0, 0 })
	for _, line := range Render(f, ASCII, 30, 10) {
		if got := strings.TrimSpace(string(cells(line))); got != "" {
			t.Fatalf("black was lifted to %q", got)
		}
	}
}

// Levels are smoothed across frames on purpose, and the property that matters
// is how a *change* is absorbed. A renderer that recomputed them per frame
// would snap to new lighting instantly, so every time anything moved in shot
// the whole picture would jump a step in brightness.
func TestLevelsEaseIntoAChange(t *testing.T) {
	dim := solid(WireW, WireH, func(x, y int) (uint8, uint8, uint8) {
		v := uint8(100 + x*20/WireW)
		return v, v, v
	})
	bright := solid(WireW, WireH, func(x, y int) (uint8, uint8, uint8) {
		v := uint8(30 + x*200/WireW)
		return v, v, v
	})

	// A renderer that has only ever seen the bright picture: the destination.
	settled := strings.Join(NewRenderer(ASCII).Draw(bright, 40, 12), "\n")

	// One that has settled on the dim picture, then meets the bright one.
	r := NewRenderer(ASCII)
	for i := 0; i < 40; i++ {
		r.Draw(dim, 40, 12)
	}
	first := strings.Join(r.Draw(bright, 40, 12), "\n")
	if first == settled {
		t.Error("the first bright frame rendered as if fully adjusted; levels are not smoothed")
	}

	var last string
	for i := 0; i < 200; i++ {
		last = strings.Join(r.Draw(bright, 40, 12), "\n")
	}
	if last != settled {
		t.Error("levels never converged on the new picture")
	}
}

// An unchanging picture must render identically every time. Any drift here
// shows up as a still scene that shimmers.
func TestSettledRendererIsStable(t *testing.T) {
	f := solid(WireW, WireH, func(x, y int) (uint8, uint8, uint8) {
		v := uint8(100 + x*20/WireW)
		return v, v, v
	})
	r := NewRenderer(Blocks)
	for i := 0; i < 50; i++ {
		r.Draw(f, 40, 12)
	}
	a := strings.Join(r.Draw(f, 40, 12), "\n")
	b := strings.Join(r.Draw(f, 40, 12), "\n")
	if a != b {
		t.Error("a settled renderer still changes on an unchanging picture")
	}
}
