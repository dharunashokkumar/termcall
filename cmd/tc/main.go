// Command tc is termcall: terminal chat rooms and ASCII video calls.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"

	"github.com/dharunashokkumar/termcall/internal/app"
	"github.com/dharunashokkumar/termcall/internal/client"
	"github.com/dharunashokkumar/termcall/internal/ui"
)

// Set at build time: -ldflags "-X main.version=v1.2.3".
var version = "dev"

const usage = `termcall — chat rooms and ASCII video calls in your terminal.

usage: tc [flags]

  -version   print the version and exit
  -camera    check the camera and try the render modes, without a call
  -host      the termcall server to use (default %s)
             the TC_HOST environment variable does the same

Run with no flags for the menu.
`

func main() {
	var showVersion, cameraOnly bool
	var host string
	flag.Usage = func() { fmt.Fprintf(os.Stderr, usage, client.DefaultHost) }
	flag.BoolVar(&showVersion, "version", false, "print the version and exit")
	flag.BoolVar(&cameraOnly, "camera", false, "check the camera, without a call")
	flag.StringVar(&host, "host", "", "the termcall server to use")
	flag.Parse()

	if showVersion {
		fmt.Println("termcall " + version)
		return
	}
	if host != "" {
		os.Setenv("TC_HOST", host)
	}

	if err := run(cameraOnly); err != nil {
		fmt.Fprintln(os.Stderr, "tc: "+err.Error())
		os.Exit(1)
	}
}

func run(cameraOnly bool) error {
	// SIGINT does not arrive in raw mode -- ctrl-c is read as a key instead --
	// but a terminal closing or a `kill` still has to restore the screen,
	// because a process that dies in raw mode leaves the shell unusable.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	t, err := ui.Open()
	if err != nil {
		return err
	}
	defer t.Close()

	a := app.New(ctx, t)
	if cameraOnly {
		a.Preview()
		return nil
	}
	return a.Run()
}
