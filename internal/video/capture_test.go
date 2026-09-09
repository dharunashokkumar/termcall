package video

import (
	"context"
	"os"
	"testing"
	"time"
)

// Capture needs a real webcam, so it is opt-in:
//
//	TC_CAMERA=1 go test ./internal/video/ -run Camera -v
//
// CI does not set it. A build machine has no camera, and a test that quietly
// passes because it skipped is worth more than one that fails for a reason
// nobody can act on.
func camera(t *testing.T) {
	t.Helper()
	if os.Getenv("TC_CAMERA") == "" {
		t.Skip("set TC_CAMERA=1 to test against a real webcam")
	}
	if _, err := FFmpeg(); err != nil {
		t.Skipf("no ffmpeg: %v", err)
	}
}

func TestCameraFindsADevice(t *testing.T) {
	camera(t)
	bin, _ := FFmpeg()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	found, err := listDevices(ctx, bin)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) == 0 {
		t.Fatal("no capture devices listed")
	}
	for _, d := range found {
		t.Logf("found %q via %v", d.name, d.args)
		if d.name == "" || len(d.args) == 0 {
			t.Errorf("device %+v is not openable", d)
		}
	}
}

func TestCameraProducesFrames(t *testing.T) {
	camera(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cam, err := OpenCamera(ctx)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer cam.Close()

	var got int
	var last *Frame
	deadline := time.After(20 * time.Second)
	for got < 3 {
		select {
		case f, ok := <-cam.Frames():
			if !ok {
				t.Fatalf("capture stopped after %d frames: %v", got, cam.Err())
			}
			if f.W != WireW || f.H != WireH {
				t.Fatalf("frame is %dx%d, want %dx%d", f.W, f.H, WireW, WireH)
			}
			if len(f.Pix) != WireW*WireH*3 {
				t.Fatalf("frame carries %d bytes, want %d", len(f.Pix), WireW*WireH*3)
			}
			last, got = f, got+1
		case <-deadline:
			t.Fatalf("only %d frames in 20s: %v", got, cam.Err())
		}
	}

	// Render what the camera actually saw, so a failure here shows up as a
	// picture in the test log rather than a number.
	for _, m := range []Mode{Blocks, ASCII, Braille} {
		t.Logf("--- %v ---", m)
		for _, line := range Render(last, m, 48, 16) {
			t.Log(line)
		}
	}
}
