package ui

import "fmt"

const (
	altOn      = "\x1b[?1049h"
	altOff     = "\x1b[?1049l"
	hideCursor = "\x1b[?25l"
	showCursor = "\x1b[?25h"
	clearAll   = "\x1b[2J"
	home       = "\x1b[H"
	reset      = "\x1b[0m"
)

const (
	Reset = "\x1b[0m"
	Bold  = "\x1b[1m"
	Dim   = "\x1b[2m"
)

// The palette is deliberately small and drawn from the 256-colour cube rather
// than truecolour, because these are chrome -- menus, names, rules -- and must
// stay legible in a terminal with an unknown theme. The video renderer uses
// full 24-bit colour; nothing else does.
const (
	Accent = "\x1b[38;5;114m" // green, the one colour that carries meaning
	Grey   = "\x1b[38;5;244m"
	Faint  = "\x1b[38;5;238m"
	Red    = "\x1b[38;5;174m"
	Yellow = "\x1b[38;5;180m"
)

// Names get a stable colour so a busy room stays readable. Blues and purples
// are excluded: they collide with Accent's role and with most terminals' own
// prompt colours.
var nameColours = []string{
	"\x1b[38;5;110m", "\x1b[38;5;180m", "\x1b[38;5;150m", "\x1b[38;5;174m",
	"\x1b[38;5;186m", "\x1b[38;5;146m", "\x1b[38;5;144m", "\x1b[38;5;181m",
}

func NameColour(name string) string {
	var h uint32 = 2166136261
	for _, b := range []byte(name) {
		h ^= uint32(b)
		h *= 16777619
	}
	return nameColours[h%uint32(len(nameColours))]
}

func Fg(c string, s string) string { return c + s + Reset }

// TrueFg and TrueBg are 24-bit colour, used only by the video renderer.
func TrueFg(r, g, b uint8) string { return fmt.Sprintf("\x1b[38;2;%d;%d;%dm", r, g, b) }
func TrueBg(r, g, b uint8) string { return fmt.Sprintf("\x1b[48;2;%d;%d;%dm", r, g, b) }
