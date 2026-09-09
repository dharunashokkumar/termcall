// Package ui is the terminal: raw mode, the alternate screen, key decoding
// and a frame buffer. It knows nothing about rooms.
//
// Frames are written whole. Every draw positions the cursor explicitly and
// clears to end of line as it goes, so a repaint never needs to blank the
// screen first -- clearing then drawing is what makes a terminal flicker.
package ui

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"sync"
	"time"

	"golang.org/x/term"
)

type Term struct {
	out    *bufio.Writer
	fd     int
	old    *term.State
	keys   chan Key
	resize chan Size
	stop   chan struct{}
	once   sync.Once

	mu   sync.Mutex
	size Size
}

type Size struct{ W, H int }

// Open puts the terminal into raw mode on the alternate screen and starts
// reading keys. Close must be called or the shell is left unusable.
func Open() (*Term, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return nil, fmt.Errorf("termcall needs a terminal")
	}
	if err := enableVT(); err != nil {
		return nil, err
	}
	old, err := term.MakeRaw(fd)
	if err != nil {
		return nil, err
	}
	w, h, err := term.GetSize(fd)
	if err != nil || w <= 0 || h <= 0 {
		w, h = 80, 24
	}

	t := &Term{
		out:    bufio.NewWriterSize(os.Stdout, 1<<16),
		fd:     fd,
		old:    old,
		keys:   make(chan Key, 64),
		resize: make(chan Size, 4),
		stop:   make(chan struct{}),
		size:   Size{w, h},
	}
	t.out.WriteString(altOn + hideCursor)
	t.out.Flush()

	go t.readKeys()
	go t.watchSize()
	return t, nil
}

func (t *Term) Close() {
	t.once.Do(func() {
		close(t.stop)
		t.out.WriteString(reset + showCursor + altOff)
		t.out.Flush()
		term.Restore(t.fd, t.old)
	})
}

func (t *Term) Size() Size {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.size
}

func (t *Term) Keys() <-chan Key     { return t.keys }
func (t *Term) Resized() <-chan Size { return t.resize }

// Terminal size is polled rather than taken from SIGWINCH, because Windows
// has no such signal and one code path beats two.
func (t *Term) watchSize() {
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-t.stop:
			return
		case <-tick.C:
			w, h, err := term.GetSize(t.fd)
			if err != nil || w <= 0 || h <= 0 {
				continue
			}
			t.mu.Lock()
			changed := w != t.size.W || h != t.size.H
			t.size = Size{w, h}
			t.mu.Unlock()
			if changed {
				select {
				case t.resize <- Size{w, h}:
				default:
				}
			}
		}
	}
}

// Buf is one frame under construction.
type Buf struct {
	b    bytes.Buffer
	W, H int
}

func (t *Term) NewBuf() *Buf {
	s := t.Size()
	b := &Buf{W: s.W, H: s.H}
	b.b.WriteString(home)
	return b
}

// Row writes s as row y (1-based), padding to the width so whatever was there
// before is covered.
func (b *Buf) Row(y int, s string) {
	if y < 1 || y > b.H {
		return
	}
	fmt.Fprintf(&b.b, "\x1b[%d;1H%s\x1b[K", y, s)
}

// At moves the cursor to a 1-based column and row.
func (b *Buf) At(x, y int) { fmt.Fprintf(&b.b, "\x1b[%d;%dH", y, x) }

func (b *Buf) Put(s string) { b.b.WriteString(s) }

// Flush paints the frame in one write. A partial frame reaching the terminal
// is what tearing looks like, so this is the only place output happens.
func (t *Term) Flush(b *Buf) {
	t.out.Write(b.b.Bytes())
	t.out.Flush()
}

// Clear blanks the screen. Only needed when switching between screens that
// draw different numbers of rows.
func (t *Term) Clear() {
	t.out.WriteString(reset + clearAll + home)
	t.out.Flush()
}
