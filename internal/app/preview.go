package app

import (
	"fmt"
	"time"

	"github.com/dharunashokkumar/termcall/internal/ui"
	"github.com/dharunashokkumar/termcall/internal/video"
)

// preview is the camera on its own, with no call and no network. It exists so
// that "is my camera working" and "does this look right" are answerable
// without two machines and a room code, and so the render modes can be
// compared side by side in the terminal they will actually be used in.
func (a *App) preview() {
	trail := []string{"camera"}

	cam, err := video.OpenCamera(a.ctx)
	if err != nil {
		a.note(trail, ui.Red, err.Error())
		return
	}
	defer cam.Close()

	r := video.NewRenderer(video.Blocks)
	var frame *video.Frame
	var fps rate
	var still stillness

	for {
		a.drawPreview(trail, r, frame, &fps, &still)

		select {
		case <-a.ctx.Done():
			return
		case <-a.t.Resized():
		case f, ok := <-cam.Frames():
			if !ok {
				msg := "the camera stopped"
				if e := cam.Err(); e != nil {
					msg = e.Error()
				}
				a.note(trail, ui.Red, msg)
				return
			}
			frame = f
			fps.tick()
			still.saw(f)
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
				r.Cycle()
			}
		}
	}
}

func (a *App) drawPreview(trail []string, r *video.Renderer, f *video.Frame, fps *rate, still *stillness) {
	b := a.t.NewBuf()
	top := a.header(b, trail)

	avail := b.H - top - 2
	cols, rows := video.Fit(b.W-4, avail, video.WireW, video.WireH)
	if f == nil || cols < 4 || rows < 2 {
		b.Row(top+1, pad+ui.Grey+"waiting for the camera…"+ui.Reset)
		a.t.Flush(b)
		return
	}

	// Centre the picture in what is left, so resizing the terminal does not
	// pin it to a corner.
	left := (b.W - cols) / 2
	y := top + (avail-rows)/2
	for _, line := range r.Draw(f, cols, rows) {
		b.At(left+1, y)
		b.Put(line)
		y++
	}

	status := fmt.Sprintf("%s%s%s  %s%dx%d · %.0f fps%s",
		ui.Accent, r.Mode(), ui.Reset, ui.Faint, cols, rows, fps.per(), ui.Reset)
	if warn := still.warning(); warn != "" {
		status += "   " + ui.Yellow + warn + ui.Reset
	}
	b.Row(b.H-1, pad+status)
	b.Row(b.H, pad+ui.Faint+"m  change mode · esc  back"+ui.Reset)
	a.t.Flush(b)
}

// rate is a frames-per-second counter over a sliding second.
type rate struct {
	marks []time.Time
}

func (r *rate) tick() {
	now := time.Now()
	r.marks = append(r.marks, now)
	cut := now.Add(-time.Second)
	for len(r.marks) > 0 && r.marks[0].Before(cut) {
		r.marks = r.marks[1:]
	}
}

func (r *rate) per() float64 { return float64(len(r.marks)) }

// stillness notices a picture that never changes.
//
// This is worth its own check because the failure is invisible: a closed
// privacy shutter, or a camera switched off in the OS, does not produce an
// error. ffmpeg opens the device, frames arrive on time, and every one of
// them is byte-identical to the last. Without this the person sees a plausible
// still picture and concludes the software is broken.
type stillness struct {
	last   uint64
	since  time.Time
	frozen bool
}

func (s *stillness) saw(f *video.Frame) {
	// Sampling every 97th byte is enough to tell a live sensor from a frozen
	// one: real sensor noise moves thousands of pixels a frame, and a prime
	// stride cannot fall into step with the row width.
	var h uint64 = 14695981039346656037
	for i := 0; i < len(f.Pix); i += 97 {
		h ^= uint64(f.Pix[i])
		h *= 1099511628211
	}
	now := time.Now()
	if h != s.last {
		s.last, s.since, s.frozen = h, now, false
		return
	}
	if s.since.IsZero() {
		s.since = now
	}
	if now.Sub(s.since) > 3*time.Second {
		s.frozen = true
	}
}

func (s *stillness) warning() string {
	if !s.frozen {
		return ""
	}
	return "the picture is not changing — check the camera's privacy shutter"
}
