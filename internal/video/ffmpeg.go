package video

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// FFmpeg returns a usable ffmpeg, looking in three places in order:
//
//  1. TC_FFMPEG, for anyone who wants to choose.
//  2. PATH, so a machine that already has ffmpeg pays nothing to unpack one.
//  3. The copy built into this binary, if this build carries one.
//
// Release builds carry ffmpeg so that a person downloads one file and nothing
// else. PATH is still tried first: unpacking ~80 MB to a cache directory is
// worth skipping when a perfectly good copy is already installed.
func FFmpeg() (string, error) {
	ffmpegOnce.Do(func() {
		ffmpegPath, ffmpegErr = findFFmpeg()
	})
	return ffmpegPath, ffmpegErr
}

var (
	ffmpegOnce sync.Once
	ffmpegPath string
	ffmpegErr  error
)

func findFFmpeg() (string, error) {
	if p := strings.TrimSpace(os.Getenv("TC_FFMPEG")); p != "" {
		if runnable(p) {
			return p, nil
		}
		return "", errors.New("TC_FFMPEG is set to " + p + ", which will not run")
	}
	if p, err := exec.LookPath("ffmpeg"); err == nil && runnable(p) {
		return p, nil
	}
	if p, err := unpack(); err == nil {
		return p, nil
	} else if !errors.Is(err, errNotEmbedded) {
		return "", err
	}
	return "", errNoFFmpeg
}

// runnable proves the binary starts and is really ffmpeg. A file on PATH
// called ffmpeg that is a broken symlink, a wrapper script for a container
// that is not running, or a stub from an uninstalled package all fail here
// rather than fifty lines later as an unreadable capture error.
func runnable(p string) bool {
	out, err := exec.Command(p, "-hide_banner", "-version").Output()
	return err == nil && strings.Contains(strings.ToLower(string(out)), "ffmpeg version")
}

var errNoFFmpeg = errors.New(`ffmpeg is needed for video and was not found.

install it once:
  Windows   winget install ffmpeg
  macOS     brew install ffmpeg
  Linux     sudo apt install ffmpeg

or point termcall at a copy you already have:
  TC_FFMPEG=/full/path/to/ffmpeg

Chat rooms do not need it.`)

// cacheDir is where an unpacked ffmpeg lives between runs.
func cacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "termcall")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

func exeName(stem string) string {
	if runtime.GOOS == "windows" {
		return stem + ".exe"
	}
	return stem
}
