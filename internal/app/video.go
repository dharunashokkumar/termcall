package app

import (
	"fmt"
	"time"

	"github.com/dharunashokkumar/termcall/internal/call"
	"github.com/dharunashokkumar/termcall/internal/client"
	"github.com/dharunashokkumar/termcall/internal/ui"
	"github.com/dharunashokkumar/termcall/internal/video"
)

// video runs a call: the camera going out to everyone, everyone else's
// pictures coming back, all of it drawn as characters.
func (a *App) video(room *client.Room) {
	trail := []string{"video call", room.Code}

	cam, err := video.OpenCamera(a.ctx)
	if err != nil {
		a.note(trail, ui.Red, err.Error())
		return
	}
	defer cam.Close()

	mesh, err := call.Start(a.ctx, room)
	if err != nil {
		a.note(trail, ui.Red, "could not set up the call:\n"+err.Error())
		return
	}
	defer mesh.Close()

	s := &callScreen{
		room: room,
		mesh: mesh,
		mode: video.Blocks,
		draw: map[string]*video.Renderer{},
	}

	// Drawing is driven by a ticker rather than by events. Events arrive in
	// bursts -- four peers' frames land together -- and redrawing on each
	// would spend the whole frame budget painting the same picture repeatedly.
	tick := time.NewTicker(time.Second / video.FPS)
	defer tick.Stop()

	for {
		select {
		case <-a.ctx.Done():
			return

		case <-a.t.Resized():

		case <-tick.C:
			a.drawCall(s)

		case f, ok := <-cam.Frames():
			if !ok {
				msg := "the camera stopped"
				if e := cam.Err(); e != nil {
					msg = e.Error()
				}
				a.note(trail, ui.Red, msg)
				return
			}
			s.mine = f
			s.fps.tick()
			s.still.saw(f)
			mesh.Send(f)

		case e, ok := <-room.Events():
			if !ok {
				msg := "the connection closed"
				if err := room.Err(); err != nil {
					msg = "connection lost: " + err.Error()
				}
				a.note(trail, ui.Red, msg)
				return
			}
			mesh.Handle(e)

		case err := <-mesh.Errs():
			s.problem = err.Error()

		case k, ok := <-a.t.Keys():
			if !ok || k.IsQuit() {
				return
			}
			switch {
			case k.Code == ui.KeyEsc:
				return
			case k.Code == ui.KeyRune && (k.R == 'q' || k.R == 'b'):
				return
			case k.Code == ui.KeyRune && k.R == 'm':
				s.mode = s.mode.Next()
				// One mode for the whole screen, applied to every renderer:
				// tiles in different modes would read as a fault, not a
				// feature.
				for _, r := range s.draw {
					r.SetMode(s.mode)
				}
			}
		}
	}
}

type callScreen struct {
	room *client.Room
	mesh *call.Mesh

	mine    *video.Frame
	mode    video.Mode
	fps     rate
	still   stillness
	problem string

	// One renderer per person, because auto-levels is per picture. Sharing
	// one would let a peer sitting in bright sunlight set the exposure for
	// everybody in a dark room.
	draw map[string]*video.Renderer
}

func (s *callScreen) rendererFor(id string) *video.Renderer {
	r, ok := s.draw[id]
	if !ok {
		r = video.NewRenderer(s.mode)
		s.draw[id] = r
	}
	return r
}

// tile is one person's rectangle on screen.
type tile struct {
	name  string
	note  string
	frame *video.Frame
	id    string
}

func (a *App) drawCall(s *callScreen) {
	b := a.t.NewBuf()
	w, h := b.W, b.H
	if w < 32 || h < 10 {
		b.Row(1, "terminal too small for a call")
		a.t.Flush(b)
		return
	}

	b.Row(1, pad+roomHeader(s.room, w-len(pad)*2))
	b.Row(2, pad+ui.Faint+rule(w-4)+ui.Reset)

	// Self first, so your own picture never moves when someone joins.
	tiles := []tile{{id: "me", name: s.room.Me.Name + " (you)", frame: s.mine}}
	if s.still.warning() != "" {
		tiles[0].note = "shutter?"
	}
	for _, t := range s.mesh.Tiles() {
		n := ""
		switch {
		case t.State != call.Live:
			n = t.State.String()
		case t.Stale:
			n = "stalled"
		}
		tiles = append(tiles, tile{id: t.ID, name: t.Name, note: n, frame: t.Frame})
	}

	top, bottom := 3, h-2
	a.layout(b, s, tiles, top, bottom)

	status := fmt.Sprintf("%s%s%s  %s%.0f fps%s",
		ui.Accent, s.mode, ui.Reset, ui.Faint, s.fps.per(), ui.Reset)
	if s.problem != "" {
		status += "   " + ui.Red + s.problem + ui.Reset
	} else if s.still.warning() != "" {
		status += "   " + ui.Yellow + s.still.warning() + ui.Reset
	}
	b.Row(h-1, pad+status)
	b.Row(h, pad+ui.Faint+"m  change mode · esc  leave"+ui.Reset)
	a.t.Flush(b)
}

// layout places up to four tiles: one fills the space, two sit side by side,
// three or four make a 2x2. Anything more is refused by the server, so there
// is no case for it here.
func (a *App) layout(b *ui.Buf, s *callScreen, tiles []tile, top, bottom int) {
	n := len(tiles)
	if n == 0 {
		return
	}
	cols, rows := 1, 1
	switch {
	case n == 2:
		cols = 2
	case n >= 3:
		cols, rows = 2, 2
	}

	availW, availH := b.W-2, bottom-top+1
	cellW, cellH := availW/cols, availH/rows

	for i, t := range tiles {
		gx, gy := i%cols, i/cols
		x0 := 2 + gx*cellW
		y0 := top + gy*cellH

		// One row of every cell is the name, so a face is always attributable.
		vw, vh := video.Fit(cellW-2, cellH-1, video.WireW, video.WireH)

		if t.frame != nil && vw >= 4 && vh >= 2 {
			r := s.rendererFor(t.id)
			lines := r.Draw(t.frame, vw, vh)
			ox := x0 + (cellW-vw)/2
			oy := y0 + (cellH-1-vh)/2
			for j, line := range lines {
				b.At(ox, oy+j)
				b.Put(line)
			}
		} else {
			msg := "connecting…"
			if t.frame == nil && t.note != "" {
				msg = t.note
			}
			b.At(x0+(cellW-len(msg))/2, y0+cellH/2)
			b.Put(ui.Faint + msg + ui.Reset)
		}

		label := ui.NameColour(t.name) + t.name + ui.Reset
		if t.note != "" {
			label += ui.Faint + "  " + t.note + ui.Reset
		}
		b.At(x0+max(0, (cellW-visLen(label))/2), y0+cellH-1)
		b.Put(label)
	}
}

func rule(n int) string {
	if n < 1 {
		return ""
	}
	s := make([]rune, n)
	for i := range s {
		s[i] = '─'
	}
	return string(s)
}
