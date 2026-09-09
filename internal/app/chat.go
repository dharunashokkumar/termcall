package app

import (
	"strconv"
	"strings"

	"github.com/dharunashokkumar/termcall/internal/client"
	"github.com/dharunashokkumar/termcall/internal/proto"
	"github.com/dharunashokkumar/termcall/internal/ui"
)

// Names sit right-aligned in a fixed gutter so the text they introduce starts
// at the same column on every line. A ragged left edge is what makes a busy
// transcript hard to skim.
const (
	timeW  = 5
	nameW  = 10
	gutter = timeW + 2 + nameW + 2
)

type line struct {
	ts     int64
	who    string // empty for a system line
	text   string
	system bool
}

// chat runs a chat room until the person leaves or the connection drops.
//
// Messages are kept as structured lines and re-wrapped on every frame rather
// than stored pre-wrapped, so resizing the terminal reflows the history
// instead of leaving it broken at the old width.
func (a *App) chat(room *client.Room) {
	log := []line{{ts: 0, system: true,
		text: "you are in " + room.Code + " — share that code to let someone in"}}
	var input []rune
	scroll := 0 // lines held back from the bottom; 0 means following

	add := func(l line) {
		log = append(log, l)
		if len(log) > 500 {
			log = log[len(log)-500:]
		}
	}

	for {
		a.drawChat(room, log, input, &scroll)

		select {
		case <-a.ctx.Done():
			return

		case <-a.t.Resized():

		case e, ok := <-room.Events():
			if !ok {
				msg := "the connection closed"
				if err := room.Err(); err != nil {
					msg = "connection lost: " + err.Error()
				}
				a.note([]string{"chat room", room.Code}, ui.Red, msg)
				return
			}
			switch e.T {
			case "msg":
				add(line{ts: e.TS, who: e.From, text: e.Text})
			case "join":
				add(line{system: true, text: e.Who.Name + " joined"})
			case "part":
				add(line{system: true, text: e.Who.Name + " left"})
			}

		case k, ok := <-a.t.Keys():
			if !ok || k.IsQuit() {
				return
			}
			switch k.Code {
			case ui.KeyEsc:
				return
			case ui.KeyUp:
				scroll++
			case ui.KeyDown:
				if scroll > 0 {
					scroll--
				}
			case ui.KeyBack:
				if len(input) > 0 {
					input = input[:len(input)-1]
				}
			case ui.KeyEnter:
				text := strings.TrimSpace(string(input))
				input = input[:0]
				if text == "" {
					continue
				}
				// The line is not echoed locally. It comes back off the
				// broadcast like everyone else's, which is the only way the
				// order on your screen matches the order on theirs.
				if err := room.Say(text); err != nil {
					add(line{system: true, text: "not sent: " + err.Error()})
				}
				scroll = 0
			case ui.KeyRune:
				if len(input) < proto.TextMax {
					input = append(input, k.R)
				}
			}
		}
	}
}

func (a *App) drawChat(room *client.Room, log []line, input []rune, scroll *int) {
	b := a.t.NewBuf()
	w, h := b.W, b.H
	if w < 30 || h < 8 {
		b.Row(1, "terminal too small")
		a.t.Flush(b)
		return
	}

	b.Row(1, pad+roomHeader(room, w-len(pad)*2))
	b.Row(2, pad+ui.Faint+strings.Repeat("─", max(0, w-4))+ui.Reset)

	top, bottom := 3, h-3
	rows := bottom - top + 1

	rendered := renderLog(log, w-len(pad)-gutter)
	// Clamp before use: the history may have shrunk, or the window grown, so
	// a scroll offset saved from a previous frame can point past the end.
	maxScroll := max(0, len(rendered)-rows)
	if *scroll > maxScroll {
		*scroll = maxScroll
	}
	start := max(0, len(rendered)-rows-*scroll)
	end := min(len(rendered), start+rows)

	y := top
	for _, r := range rendered[start:end] {
		b.Row(y, pad+r)
		y++
	}

	b.Row(h-2, pad+ui.Faint+strings.Repeat("─", max(0, w-4))+ui.Reset)

	// The input scrolls horizontally once it outgrows the line, keeping the
	// cursor and the tail of what you typed in view.
	shown := []rune(string(input))
	avail := w - len(pad) - 4
	if len(shown) > avail {
		shown = shown[len(shown)-avail:]
	}
	b.Row(h-1, pad+ui.Accent+"› "+ui.Reset+string(shown)+ui.Accent+"█"+ui.Reset)

	hint := "enter send · ↑↓ scroll · esc leave"
	if *scroll > 0 {
		hint = ui.Yellow + "scrolled back " + strconv.Itoa(*scroll) + " lines" + ui.Reset +
			ui.Faint + " · ↓ to follow again" + ui.Reset
	}
	b.Row(h, pad+ui.Faint+hint+ui.Reset)
	a.t.Flush(b)
}

// renderLog turns messages into screen lines, wrapping the text to width and
// indenting continuations under the first line's text.
func renderLog(log []line, width int) []string {
	if width < 8 {
		width = 8
	}
	indent := strings.Repeat(" ", gutter)
	var out []string
	for _, l := range log {
		head, tint := "", ""
		if l.system {
			head = ui.Faint + clock(l.ts) + ui.Reset + strings.Repeat(" ", nameW+4)
			tint = ui.Faint
		} else {
			name := l.who
			if len([]rune(name)) > nameW {
				name = string([]rune(name)[:nameW])
			}
			head = ui.Faint + clock(l.ts) + ui.Reset + "  " +
				strings.Repeat(" ", nameW-len([]rune(name))) +
				ui.NameColour(l.who) + name + ui.Reset + "  "
		}
		for i, seg := range wrap(l.text, width) {
			prefix := indent
			if i == 0 {
				prefix = head
			}
			if tint != "" {
				seg = tint + seg + ui.Reset
			}
			out = append(out, prefix+seg)
		}
	}
	return out
}

// wrap breaks plain text at word boundaries, falling back to a hard break for
// a single word longer than the line.
func wrap(s string, width int) []string {
	if s == "" {
		return []string{""}
	}
	var out []string
	for _, para := range strings.Split(s, "\n") {
		cur := ""
		for _, word := range strings.Fields(para) {
			switch {
			case cur == "":
				cur = word
			case len([]rune(cur))+1+len([]rune(word)) <= width:
				cur += " " + word
			default:
				out = append(out, cur)
				cur = word
			}
			for len([]rune(cur)) > width {
				r := []rune(cur)
				out = append(out, string(r[:width]))
				cur = string(r[width:])
			}
		}
		out = append(out, cur)
	}
	return out
}
