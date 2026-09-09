//go:build embedffmpeg

package video

import (
	"os"
	"os/exec"
	"testing"
)

// The embedded copy is the whole point of shipping one file, and the only way
// to know it survived the build is to unpack it and run it. This test only
// exists in builds that carry one.
func TestEmbeddedFFmpegRuns(t *testing.T) {
	path, err := unpack()
	if err != nil {
		t.Fatalf("unpack: %v", err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("unpacked %s (%.1f MB)", path, float64(fi.Size())/(1<<20))

	out, err := exec.Command(path, "-hide_banner", "-version").Output()
	if err != nil {
		t.Fatalf("the unpacked ffmpeg will not run: %v", err)
	}
	t.Logf("%s", firstLine(string(out)))

	// Unpacking twice must reuse the file rather than write it again: the
	// name is the hash of the contents, so the second call is a stat.
	again, err := unpack()
	if err != nil || again != path {
		t.Fatalf("second unpack gave (%q, %v), want the same path", again, err)
	}
}

// With no ffmpeg on PATH, the lookup has to fall through to the embedded copy
// rather than telling the person to install one they already have.
func TestFallsBackToEmbedded(t *testing.T) {
	t.Setenv("PATH", "")
	t.Setenv("TC_FFMPEG", "")
	got, err := findFFmpeg()
	if err != nil {
		t.Fatalf("with an empty PATH the embedded copy should be used: %v", err)
	}
	if !runnable(got) {
		t.Fatalf("findFFmpeg returned %q, which will not run", got)
	}
}

func firstLine(s string) string {
	for i, r := range s {
		if r == '\n' || r == '\r' {
			return s[:i]
		}
	}
	return s
}
