// Package app is the screens: the menus, the prompts and the rooms.
//
// Every screen is a function that owns the terminal until it returns. There is
// no screen stack and no router -- the flow is shallow enough that the Go call
// stack is the flow, and reading Run top to bottom tells you the whole app.
package app

import (
	"context"
	"strings"

	"github.com/dharunashokkumar/termcall/internal/proto"
	"github.com/dharunashokkumar/termcall/internal/ui"
)

type App struct {
	t   *ui.Term
	ctx context.Context
}

func New(ctx context.Context, t *ui.Term) *App { return &App{t: t, ctx: ctx} }

// Preview runs the camera screen on its own, for `tc -camera`.
func (a *App) Preview() { a.preview() }

const pad = "  "

func (a *App) Run() error {
	for {
		switch a.menu(nil, []item{
			{'1', "chat room", "talk to up to 10 people"},
			{'2', "video call", "ASCII video, up to 4 people"},
			{'3', "camera check", "see yourself, try the render modes"},
		}) {
		case 0:
			a.roomFlow(proto.Chat)
		case 1:
			a.roomFlow(proto.Video)
		case 2:
			a.preview()
		default:
			return nil
		}
	}
}

// roomFlow is create-or-join, and is identical for both kinds of room. The
// only thing the mode changes is the label, the capacity and which screen is
// handed the connection at the end.
func (a *App) roomFlow(mode proto.Mode) {
	name := "chat room"
	if mode == proto.Video {
		name = "video call"
	}
	for {
		switch a.menu([]string{name}, []item{
			{'1', "create a room", "start one and get a code to share"},
			{'2', "join a room", "you have a code from someone"},
		}) {
		case 0:
			a.create(mode, name)
		case 1:
			a.join(mode, name)
		default:
			return
		}
	}
}

// ---------------------------------------------------------------- chrome

// header draws the brand and the trail of screens above it, and returns the
// first row a screen may use for its own content.
func (a *App) header(b *ui.Buf, trail []string) int {
	line := ui.Bold + "term" + ui.Accent + "call" + ui.Reset
	for _, s := range trail {
		line += ui.Faint + "  ›  " + ui.Reset + ui.Grey + s + ui.Reset
	}
	b.Row(2, pad+line)
	return 4
}

func (a *App) footer(b *ui.Buf, hint string) {
	b.Row(b.H-1, pad+ui.Faint+hint+ui.Reset)
}

// ---------------------------------------------------------------- menu

type item struct {
	key   rune
	label string
	hint  string
}

// menu returns the index chosen, or -1 for back/quit. Both the number key and
// the arrows work, because people reach for whichever they already trust.
func (a *App) menu(trail []string, items []item) int {
	sel := 0
	for {
		b := a.t.NewBuf()
		row := a.header(b, trail)
		row++
		for i, it := range items {
			cursor, label := "  ", ui.Grey
			if i == sel {
				cursor, label = ui.Accent+"› "+ui.Reset, ui.Reset
			}
			line := cursor + ui.Accent + string(it.key) + ui.Reset + "  " + label + it.label + ui.Reset
			if i == sel && it.hint != "" {
				line += ui.Faint + "   " + it.hint + ui.Reset
			}
			b.Row(row, pad+line)
			row++
		}
		row++
		back := "q  quit"
		if len(trail) > 0 {
			back = "b  back"
		}
		b.Row(row, pad+"  "+ui.Faint+back+ui.Reset)
		a.footer(b, "↑↓ move · enter choose")
		a.t.Flush(b)

		select {
		case <-a.ctx.Done():
			return -1
		case <-a.t.Resized():
		case k, ok := <-a.t.Keys():
			if !ok || k.IsQuit() {
				return -1
			}
			switch k.Code {
			case ui.KeyUp:
				sel = (sel - 1 + len(items)) % len(items)
			case ui.KeyDown:
				sel = (sel + 1) % len(items)
			case ui.KeyEnter:
				return sel
			case ui.KeyEsc:
				return -1
			case ui.KeyRune:
				if k.R == 'q' || k.R == 'b' {
					return -1
				}
				for i, it := range items {
					if k.R == it.key {
						return i
					}
				}
			}
		}
	}
}

// ---------------------------------------------------------------- prompt

// prompt reads one line. clean both validates and canonicalises, so the caller
// never sees a value it would have to check again. Returning ok false means
// the person backed out.
func (a *App) prompt(trail []string, label, seed string, clean func(string) (string, error)) (string, bool) {
	in := []rune(seed)
	var problem string
	for {
		b := a.t.NewBuf()
		row := a.header(b, trail)
		row++
		b.Row(row, pad+ui.Grey+label+"  "+ui.Reset+string(in)+ui.Accent+"█"+ui.Reset)
		if problem != "" {
			b.Row(row+2, pad+ui.Red+problem+ui.Reset)
		}
		a.footer(b, "enter continue · esc back")
		a.t.Flush(b)

		select {
		case <-a.ctx.Done():
			return "", false
		case <-a.t.Resized():
		case k, ok := <-a.t.Keys():
			if !ok || k.IsQuit() {
				return "", false
			}
			switch k.Code {
			case ui.KeyEsc:
				return "", false
			case ui.KeyBack:
				if len(in) > 0 {
					in = in[:len(in)-1]
				}
				problem = ""
			case ui.KeyEnter:
				v, err := clean(string(in))
				if err != nil {
					problem = err.Error()
					continue
				}
				return v, true
			case ui.KeyRune:
				if len(in) < 64 {
					in = append(in, k.R)
				}
				problem = ""
			}
		}
	}
}

// note paints a single message and waits, for the cases where something went
// wrong and the person has nothing to do but read it.
func (a *App) note(trail []string, colour, msg string) {
	b := a.t.NewBuf()
	row := a.header(b, trail)
	row++
	for _, line := range strings.Split(msg, "\n") {
		b.Row(row, pad+colour+line+ui.Reset)
		row++
	}
	a.footer(b, "any key to go back")
	a.t.Flush(b)
	select {
	case <-a.ctx.Done():
	case <-a.t.Keys():
	}
}
