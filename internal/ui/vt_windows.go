//go:build windows

package ui

import (
	"os"

	"golang.org/x/sys/windows"
)

// Windows consoles only interpret ANSI escapes when the output handle asks
// for it. Windows Terminal turns this on already; conhost.exe does not, and
// without it every escape we write appears on screen as literal text.
func enableVT() error {
	h := windows.Handle(os.Stdout.Fd())
	var mode uint32
	if err := windows.GetConsoleMode(h, &mode); err != nil {
		return nil // not a console we can configure; assume it copes
	}
	return windows.SetConsoleMode(h, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING)
}
