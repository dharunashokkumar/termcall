package video

import (
	"strings"

	"github.com/dharunashokkumar/termcall/internal/ui"
)

type Mode int

const (
	Blocks  Mode = iota // ▀ with two colours: the most picture per cell
	ASCII               // one character per cell, chosen by brightness
	Braille             // 2x4 dots per cell: the most detail, least colour
	numModes
)

func (m Mode) String() string {
	switch m {
	case ASCII:
		return "ascii"
	case Braille:
		return "braille"
	default:
		return "blocks"
	}
}

// Next cycles the modes, which is what the m key does during a call.
func (m Mode) Next() Mode { return (m + 1) % numModes }

// Dots is how many image samples one terminal cell carries, across and down.
func (m Mode) Dots() (int, int) {
	switch m {
	case ASCII:
		return 1, 1
	case Braille:
		return 2, 4
	default:
		return 1, 2
	}
}

// Fit chooses a cell grid inside the space available that keeps the picture's
// shape.
//
// The correction is screen geometry, not a property of the mode: a terminal
// cell is about twice as tall as it is wide, so a 4:3 picture needs roughly
// 2.7 columns for every row it uses. Ignore that and every face is stretched.
func Fit(cols, rows, srcW, srcH int) (int, int) {
	if cols < 1 || rows < 1 || srcW < 1 || srcH < 1 {
		return 0, 0
	}
	w := rows * 2 * srcW / srcH
	if w <= cols {
		return w, rows
	}
	return cols, cols * srcH / (2 * srcW)
}

// grid is the picture resampled to exactly the samples a mode needs.
type grid struct {
	w, h int
	px   []uint8 // RGB
}

func (g *grid) at(x, y int) (uint8, uint8, uint8) {
	i := (y*g.w + x) * 3
	return g.px[i], g.px[i+1], g.px[i+2]
}

func sample(f *Frame, w, h int) *grid {
	g := &grid{w: w, h: h, px: make([]uint8, w*h*3)}
	for y := 0; y < h; y++ {
		y0, y1 := y*f.H/h, (y+1)*f.H/h
		for x := 0; x < w; x++ {
			x0, x1 := x*f.W/w, (x+1)*f.W/w
			r, gg, b := f.box(x0, y0, x1, y1)
			i := (y*w + x) * 3
			g.px[i], g.px[i+1], g.px[i+2] = r, gg, b
		}
	}
	return g
}

// Renderer draws a stream of frames, carrying the auto-level state from one
// to the next so the picture settles instead of pulsing.
type Renderer struct {
	mode Mode
	lv   levels
}

func NewRenderer(m Mode) *Renderer { return &Renderer{mode: m} }

func (r *Renderer) Mode() Mode     { return r.mode }
func (r *Renderer) SetMode(m Mode) { r.mode = m }

// Cycle advances to the next mode and reports it. This is the m key.
func (r *Renderer) Cycle() Mode {
	r.mode = r.mode.Next()
	return r.mode
}

// Draw renders one frame as cols x rows terminal cells, one string per row.
// Every row ends reset, so a caller can place them anywhere without colour
// leaking into whatever it draws next.
func (r *Renderer) Draw(f *Frame, cols, rows int) []string {
	if f == nil || cols < 1 || rows < 1 {
		return nil
	}
	dw, dh := r.mode.Dots()
	g := sample(f, cols*dw, rows*dh)
	r.lv.update(g)
	r.lv.apply(g)
	switch r.mode {
	case ASCII:
		return renderASCII(g, cols, rows)
	case Braille:
		return renderBraille(g, cols, rows)
	default:
		return renderBlocks(g, cols, rows)
	}
}

// Render draws a single frame with no history. Anything drawing successive
// frames should hold a Renderer instead, or the auto-levels restart from
// nothing every frame and the picture flickers.
func Render(f *Frame, m Mode, cols, rows int) []string {
	return NewRenderer(m).Draw(f, cols, rows)
}

// A run of cells usually shares a colour, and re-stating it costs ~19 bytes
// each time. Emitting only changes is what keeps a frame small enough to
// write in one syscall without the terminal visibly filling in.
type painter struct {
	sb         strings.Builder
	fr, fg, fb int
	br, bg, bb int
}

func newPainter() *painter {
	p := &painter{}
	p.reset()
	return p
}

func (p *painter) reset() {
	p.fr, p.fg, p.fb = -1, -1, -1
	p.br, p.bg, p.bb = -1, -1, -1
}

func (p *painter) setFg(r, g, b uint8) {
	if int(r) == p.fr && int(g) == p.fg && int(b) == p.fb {
		return
	}
	p.fr, p.fg, p.fb = int(r), int(g), int(b)
	p.sb.WriteString(ui.TrueFg(r, g, b))
}

func (p *painter) setBg(r, g, b uint8) {
	if int(r) == p.br && int(g) == p.bg && int(b) == p.bb {
		return
	}
	p.br, p.bg, p.bb = int(r), int(g), int(b)
	p.sb.WriteString(ui.TrueBg(r, g, b))
}

func renderBlocks(g *grid, cols, rows int) []string {
	out := make([]string, rows)
	p := newPainter()
	for y := 0; y < rows; y++ {
		p.sb.Reset()
		p.reset()
		for x := 0; x < cols; x++ {
			tr, tg, tb := g.at(x, y*2)
			br, bg, bb := g.at(x, y*2+1)
			p.setFg(tr, tg, tb)
			p.setBg(br, bg, bb)
			p.sb.WriteString("▀")
		}
		p.sb.WriteString(ui.Reset)
		out[y] = p.sb.String()
	}
	return out
}

// ramp runs dark to light. Space at the dark end is deliberate: an unlit
// cell should read as absence, and any character there would put a grey
// haze over what should be shadow.
const ramp = " .:-=+*#%@"

func renderASCII(g *grid, cols, rows int) []string {
	out := make([]string, rows)
	p := newPainter()
	for y := 0; y < rows; y++ {
		p.sb.Reset()
		p.reset()
		for x := 0; x < cols; x++ {
			r, gg, b := g.at(x, y)
			// Divide by 256, not 255: scaling to the last index instead
			// of the bucket count makes the brightest character reachable
			// only at exactly 255, so a lit face renders one step darker
			// than it should while black swallows nearly a ninth of the
			// range.
			c := ramp[lum(r, gg, b)*len(ramp)/256]
			if c != ' ' {
				p.setFg(r, gg, b)
			}
			p.sb.WriteByte(c)
		}
		p.sb.WriteString(ui.Reset)
		out[y] = p.sb.String()
	}
	return out
}

// Braille dot bits, indexed [column][row] within the 2x4 cell. The numbering
// is the standard one and is not sequential: dots 7 and 8 were added to the
// original six-dot cell, so the bottom row carries the two high bits.
var dotBit = [2][4]byte{
	{0x01, 0x02, 0x04, 0x40},
	{0x08, 0x10, 0x20, 0x80},
}

func renderBraille(g *grid, cols, rows int) []string {
	// A single global threshold turns a dim room into a black rectangle and a
	// bright one into a solid block. Comparing each cell against its own mean
	// instead is what makes this mode draw edges -- but only where there is
	// an edge to draw, so flat cells fall back to the frame's own average or
	// they would fill with noise.
	var total, n int
	for i := 0; i < len(g.px); i += 3 {
		total += lum(g.px[i], g.px[i+1], g.px[i+2])
		n++
	}
	global := 0
	if n > 0 {
		global = total / n
	}

	out := make([]string, rows)
	p := newPainter()
	for y := 0; y < rows; y++ {
		p.sb.Reset()
		p.reset()
		for x := 0; x < cols; x++ {
			var l [2][4]int
			lo, hi, sum := 255, 0, 0
			for dx := 0; dx < 2; dx++ {
				for dy := 0; dy < 4; dy++ {
					r, gg, b := g.at(x*2+dx, y*4+dy)
					v := lum(r, gg, b)
					l[dx][dy] = v
					sum += v
					lo, hi = min(lo, v), max(hi, v)
				}
			}
			cut := sum / 8
			if hi-lo < 24 {
				cut = global
			}

			var mask byte
			var lr, lg, lb, on int
			for dx := 0; dx < 2; dx++ {
				for dy := 0; dy < 4; dy++ {
					if l[dx][dy] <= cut {
						continue
					}
					mask |= dotBit[dx][dy]
					r, gg, b := g.at(x*2+dx, y*4+dy)
					lr, lg, lb = lr+int(r), lg+int(gg), lb+int(b)
					on++
				}
			}
			if on == 0 {
				p.sb.WriteRune(' ')
				continue
			}
			// Colour comes from the lit dots alone. Averaging the whole cell
			// would drag every bright stroke toward the dark it sits on.
			p.setFg(uint8(lr/on), uint8(lg/on), uint8(lb/on))
			p.sb.WriteRune(rune(0x2800 + int(mask)))
		}
		p.sb.WriteString(ui.Reset)
		out[y] = p.sb.String()
	}
	return out
}
