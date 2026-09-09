package app

import (
	"errors"
	"fmt"
	"os/user"
	"strings"
	"time"

	"github.com/dharunashokkumar/termcall/internal/client"
	"github.com/dharunashokkumar/termcall/internal/proto"
	"github.com/dharunashokkumar/termcall/internal/ui"
)

// suggestName seeds the name prompt with the account name, because the answer
// is almost always that and typing it every time is friction for nothing.
func suggestName() string {
	u, err := user.Current()
	if err != nil {
		return ""
	}
	n := u.Username
	// Windows hands back DOMAIN\user.
	if i := strings.LastIndexAny(n, `\/`); i >= 0 {
		n = n[i+1:]
	}
	n, err = proto.CleanName(n)
	if err != nil {
		return ""
	}
	return n
}

func (a *App) create(mode proto.Mode, label string) {
	trail := []string{label, "create"}
	name, ok := a.prompt(trail, "your name:", suggestName(), proto.CleanName)
	if !ok {
		return
	}

	a.working(trail, "making a room…")

	// Codes are picked here and refused by the server if they collide. At
	// 31^6 codes that is rare enough to retry rather than prevent, but not so
	// rare that skipping the retry would be honest.
	var room *client.Room
	var err error
	for try := 0; try < 5; try++ {
		var code string
		if code, err = proto.NewCode(); err != nil {
			break
		}
		room, err = client.Join(a.ctx, code, name, mode, true)
		if !errors.Is(err, client.ErrTaken) {
			break
		}
	}
	if err != nil {
		a.note(trail, ui.Red, "could not make a room:\n"+err.Error())
		return
	}
	defer room.Close()
	a.enter(room)
}

func (a *App) join(mode proto.Mode, label string) {
	trail := []string{label, "join"}
	code, ok := a.prompt(trail, "room code:", "", proto.CleanCode)
	if !ok {
		return
	}
	name, ok := a.prompt(trail, "your name:", suggestName(), proto.CleanName)
	if !ok {
		return
	}

	a.working(trail, "joining "+code+"…")
	room, err := client.Join(a.ctx, code, name, mode, false)
	if err != nil {
		a.note(trail, ui.Red, "could not join:\n"+err.Error())
		return
	}
	defer room.Close()
	a.enter(room)
}

// enter hands a live room to the screen that knows how to draw it.
func (a *App) enter(room *client.Room) {
	switch room.Mode {
	case proto.Video:
		a.video(room)
	default:
		a.chat(room)
	}
}

// working paints a message while something slow happens. Dialling has a 15
// second timeout, and a frozen screen for that long reads as a crash.
func (a *App) working(trail []string, msg string) {
	b := a.t.NewBuf()
	row := a.header(b, trail)
	row++
	b.Row(row, pad+ui.Grey+msg+ui.Reset)
	a.t.Flush(b)
}

// roomHeader is the bar every in-room screen carries: the code to share, who
// is here, and how full it is.
func roomHeader(room *client.Room, width int) string {
	kind := "chat"
	if room.Mode == proto.Video {
		kind = "call"
	}
	here := len(room.Peers()) + 1
	left := ui.Faint + kind + " " + ui.Reset + ui.Bold + ui.Accent + room.Code + ui.Reset
	right := fmt.Sprintf("%s%d/%d here%s", ui.Faint, here, room.Cap, ui.Reset)

	names := make([]string, 0, here)
	names = append(names, ui.NameColour(room.Me.Name)+room.Me.Name+ui.Faint+" (you)"+ui.Reset)
	for _, p := range room.Peers() {
		names = append(names, ui.NameColour(p.Name)+p.Name+ui.Reset)
	}
	mid := strings.Join(names, ui.Faint+" · "+ui.Reset)

	line := left + ui.Faint + "  ·  " + ui.Reset + mid
	if visLen(line)+visLen(right)+4 < width {
		line += strings.Repeat(" ", width-visLen(line)-visLen(right)-2) + right
	}
	return line
}

// visLen is the printed width of a string carrying ANSI colour, which is what
// layout has to measure. Escape sequences occupy no columns.
func visLen(s string) int {
	n, esc := 0, false
	for _, r := range s {
		switch {
		case esc:
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				esc = false
			}
		case r == 0x1b:
			esc = true
		default:
			n++
		}
	}
	return n
}

func clock(ts int64) string {
	if ts == 0 {
		return time.Now().Format("15:04")
	}
	return time.UnixMilli(ts).Format("15:04")
}
