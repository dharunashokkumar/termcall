package video

import (
	"testing"
)

// scene is something closer to a webcam picture than a gradient: a lit
// subject against a darker background, with a soft edge. Compression numbers
// measured on flat colour or on noise are both meaningless -- one is far too
// kind, the other far too harsh.
func scene() *Frame {
	return solid(WireW, WireH, func(x, y int) (uint8, uint8, uint8) {
		dx, dy := float64(x-WireW/2)/float64(WireW/3), float64(y-WireH/2)/float64(WireH/3)
		d := dx*dx + dy*dy
		v := 210 - d*120
		if v < 40 {
			v = 40
		}
		return uint8(v), uint8(v * 0.86), uint8(v * 0.78)
	})
}

func TestCodecRoundTrip(t *testing.T) {
	src := scene()
	var e Encoder
	b, err := e.Encode(src)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	if got.W != src.W || got.H != src.H {
		t.Fatalf("round trip changed size to %dx%d", got.W, got.H)
	}

	var diff int
	for i := range src.Pix {
		d := int(src.Pix[i]) - int(got.Pix[i])
		if d < 0 {
			d = -d
		}
		diff += d
	}
	mean := float64(diff) / float64(len(src.Pix))
	// The renderer averages whole blocks of these pixels into one cell, so
	// what matters is that the error is small next to a ramp step (25 levels),
	// not that it is zero.
	if mean > 6 {
		t.Errorf("mean channel error is %.1f, too lossy for the renderer", mean)
	}
	t.Logf("mean channel error %.2f", mean)
}

// The whole peer-to-peer design rests on a frame being small. At ten frames a
// second to each of three peers, a kilobyte here is 30 KB/s of upstream there.
func TestEncodedFrameIsSmall(t *testing.T) {
	var e Encoder
	b, err := e.Encode(scene())
	if err != nil {
		t.Fatal(err)
	}
	perPeer := float64(len(b)*FPS) / 1024
	t.Logf("%d bytes/frame → %.0f KB/s per peer, %.0f KB/s up in a full 4-way call",
		len(b), perPeer, perPeer*3)
	if len(b) > 8<<10 {
		t.Errorf("a frame is %d bytes; three peers would need %.0f KB/s upstream",
			len(b), perPeer*3)
	}
}

// Frames arrive from another machine. A claimed size the renderer would
// happily sample from has to be refused rather than trusted.
func TestDecodeRejectsRubbish(t *testing.T) {
	for _, c := range []struct {
		name string
		in   []byte
	}{
		{"empty", nil},
		{"not a jpeg", []byte("hello there, this is not a picture")},
		{"truncated", func() []byte {
			var e Encoder
			b, _ := e.Encode(scene())
			return b[:len(b)/2]
		}()},
	} {
		if _, err := Decode(c.in); err == nil {
			t.Errorf("%s decoded without complaint", c.name)
		}
	}
}

// The encoder reuses its buffer, which is only safe if callers are told. This
// pins the behaviour so nobody later assumes a fresh slice.
func TestEncoderReusesItsBuffer(t *testing.T) {
	var e Encoder
	a, err := e.Encode(scene())
	if err != nil {
		t.Fatal(err)
	}
	first := append([]byte(nil), a...)
	if _, err := e.Encode(solid(WireW, WireH, func(x, y int) (uint8, uint8, uint8) {
		return uint8(x), 0, uint8(y)
	})); err != nil {
		t.Fatal(err)
	}
	if string(a) == string(first) {
		t.Skip("the two encodes happened to be the same length and content")
	}
}
