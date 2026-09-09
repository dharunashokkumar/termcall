package app

import (
	"strings"
	"testing"

	"github.com/dharunashokkumar/termcall/internal/ui"
)

// visLen decides where the right-hand side of the room header starts. If it
// counted escape sequences the header would wrap, and a wrapped header pushes
// the whole transcript down by a row every frame.
func TestVisLenIgnoresColour(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"plain", 5},
		{ui.Accent + "abc" + ui.Reset, 3},
		{ui.Bold + ui.Accent + "ab" + ui.Reset + "cd", 4},
		{ui.TrueFg(1, 2, 3) + "x" + ui.TrueBg(4, 5, 6) + "y", 2},
	}
	for _, c := range cases {
		if got := visLen(c.in); got != c.want {
			t.Errorf("visLen(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestWrap(t *testing.T) {
	cases := []struct {
		name  string
		in    string
		width int
		want  []string
	}{
		{"empty stays one line", "", 10, []string{""}},
		{"short", "hi there", 20, []string{"hi there"}},
		{"breaks on words", "one two three four", 9, []string{"one two", "three", "four"}},
		{"hard breaks a long word", "aaaaaaaaaaaa", 5, []string{"aaaaa", "aaaaa", "aa"}},
		{"keeps newlines", "a\nb", 10, []string{"a", "b"}},
	}
	for _, c := range cases {
		got := wrap(c.in, c.width)
		if strings.Join(got, "|") != strings.Join(c.want, "|") {
			t.Errorf("%s: wrap(%q,%d) = %q, want %q", c.name, c.in, c.width, got, c.want)
		}
		for _, line := range got {
			if len([]rune(line)) > c.width {
				t.Errorf("%s: line %q is wider than %d", c.name, line, c.width)
			}
		}
	}
}

// Every rendered line has to start at the same column, or the transcript reads
// as ragged. Continuations are indented to the gutter; first lines carry the
// clock and name that fill it.
func TestRenderLogAligns(t *testing.T) {
	log := []line{
		{ts: 1700000000000, who: "ana", text: "a short one"},
		{ts: 1700000000000, who: "bartholomew", text: strings.Repeat("word ", 20)},
		{system: true, text: "bo left"},
	}
	out := renderLog(log, 30)
	if len(out) < 4 {
		t.Fatalf("expected the long message to wrap, got %d lines", len(out))
	}
	for i, l := range out {
		if got := visLen(l) - visLen(strings.TrimLeft(stripANSI(l), " ")); got > gutter {
			t.Errorf("line %d indents %d columns, past the %d gutter", i, got, gutter)
		}
	}
	// A name longer than the gutter must be cut, not allowed to shove the
	// text right and break the column.
	if !strings.Contains(stripANSI(out[1]), "bartholom") {
		t.Errorf("long name missing from %q", stripANSI(out[1]))
	}
	if visLen(out[1]) > 30+gutter {
		t.Errorf("long-name line is %d wide, past the limit", visLen(out[1]))
	}
}

func stripANSI(s string) string {
	var b strings.Builder
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
			b.WriteRune(r)
		}
	}
	return b.String()
}
