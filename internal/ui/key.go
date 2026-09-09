package ui

import (
	"os"
	"unicode/utf8"
)

type Code int

const (
	KeyRune Code = iota
	KeyEnter
	KeyBack
	KeyEsc
	KeyTab
	KeyUp
	KeyDown
	KeyLeft
	KeyRight
	KeyCtrl // R holds the letter, so ctrl-c is {KeyCtrl, 'c'}
)

type Key struct {
	Code Code
	R    rune
}

// readKeys turns raw stdin bytes into key events. It parses a whole read at a
// time: an escape sequence arrives as one chunk from a terminal, so a lone
// 0x1b at the end of a chunk really is the Escape key and needs no timer to
// tell it apart from the start of an arrow key.
func (t *Term) readKeys() {
	defer close(t.keys)
	buf := make([]byte, 256)
	for {
		select {
		case <-t.stop:
			return
		default:
		}
		n, err := os.Stdin.Read(buf)
		if err != nil {
			return
		}
		for _, k := range decode(buf[:n]) {
			select {
			case t.keys <- k:
			case <-t.stop:
				return
			}
		}
	}
}

func decode(p []byte) []Key {
	var out []Key
	for i := 0; i < len(p); {
		b := p[i]
		switch {
		case b == 0x1b:
			// CSI: ESC [ ... final-byte
			if i+2 < len(p) && p[i+1] == '[' {
				j := i + 2
				for j < len(p) && (p[j] < 0x40 || p[j] > 0x7e) {
					j++
				}
				if j < len(p) {
					switch p[j] {
					case 'A':
						out = append(out, Key{Code: KeyUp})
					case 'B':
						out = append(out, Key{Code: KeyDown})
					case 'C':
						out = append(out, Key{Code: KeyRight})
					case 'D':
						out = append(out, Key{Code: KeyLeft})
					}
					i = j + 1
					continue
				}
			}
			out = append(out, Key{Code: KeyEsc})
			i++
		case b == '\r' || b == '\n':
			out = append(out, Key{Code: KeyEnter})
			i++
		case b == 0x7f || b == 0x08:
			out = append(out, Key{Code: KeyBack})
			i++
		case b == '\t':
			out = append(out, Key{Code: KeyTab})
			i++
		case b < 0x20:
			out = append(out, Key{Code: KeyCtrl, R: rune(b) + 'a' - 1})
			i++
		default:
			r, sz := utf8.DecodeRune(p[i:])
			if r == utf8.RuneError && sz <= 1 {
				i++
				continue
			}
			out = append(out, Key{Code: KeyRune, R: r})
			i += sz
		}
	}
	return out
}

// IsQuit reports the two gestures that always mean "stop": ctrl-c and ctrl-d.
func (k Key) IsQuit() bool {
	return k.Code == KeyCtrl && (k.R == 'c' || k.R == 'd')
}
