package call_test

// A real handshake between two real peers, through a real Worker. Needs a
// server:
//
//	cd worker && npx wrangler dev --port 8787 --local
//	TC_HOST=http://127.0.0.1:8787 go test ./internal/call/
//
// Two peers on one machine connect over host candidates without leaving the
// box, so this exercises the whole path -- offer, answer, trickled
// candidates, data channel, JPEG both ways -- without needing anything
// outside. What it cannot prove is NAT traversal, which is what STUN and TURN
// are for and what only two machines on different networks can show.

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/dharunashokkumar/termcall/internal/call"
	"github.com/dharunashokkumar/termcall/internal/client"
	"github.com/dharunashokkumar/termcall/internal/proto"
	"github.com/dharunashokkumar/termcall/internal/video"
)

func need(t *testing.T) {
	t.Helper()
	if os.Getenv("TC_HOST") == "" {
		t.Skip("set TC_HOST to a running termcall server")
	}
}

// pump is what the call screen does in its event loop: hand every room event
// to the mesh.
func pump(ctx context.Context, r *client.Room, m *call.Mesh) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case e, ok := <-r.Events():
				if !ok {
					return
				}
				m.Handle(e)
			}
		}
	}()
}

func testFrame(seed uint8) *video.Frame {
	f := video.NewFrame(video.WireW, video.WireH)
	for y := 0; y < f.H; y++ {
		for x := 0; x < f.W; x++ {
			i := (y*f.W + x) * 3
			f.Pix[i] = uint8(x) + seed
			f.Pix[i+1] = uint8(y)
			f.Pix[i+2] = seed
		}
	}
	return f
}

func TestTwoPeersExchangeFrames(t *testing.T) {
	need(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	code, err := proto.NewCode()
	if err != nil {
		t.Fatal(err)
	}

	ana, err := client.Join(ctx, code, "ana", proto.Video, true)
	if err != nil {
		t.Fatal(err)
	}
	defer ana.Close()

	meshA, err := call.Start(ctx, ana)
	if err != nil {
		t.Fatal(err)
	}
	defer meshA.Close()
	pump(ctx, ana, meshA)

	bo, err := client.Join(ctx, code, "bo", proto.Video, false)
	if err != nil {
		t.Fatal(err)
	}
	defer bo.Close()

	// bo arrived second, so bo offers to ana. That rule is the whole of the
	// glare avoidance: for any two people, exactly one arrived second.
	meshB, err := call.Start(ctx, bo)
	if err != nil {
		t.Fatal(err)
	}
	defer meshB.Close()
	pump(ctx, bo, meshB)

	// Both keep sending, as they would in a call. The data channel is not
	// open the instant the handshake starts, so a single send would land
	// nowhere.
	go func() {
		tick := time.NewTicker(100 * time.Millisecond)
		defer tick.Stop()
		var n uint8
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				n++
				meshA.Send(testFrame(n))
				meshB.Send(testFrame(n + 128))
			}
		}
	}()

	deadline := time.After(45 * time.Second)
	for {
		aOK := hasFrame(meshA)
		bOK := hasFrame(meshB)
		if aOK && bOK {
			t.Logf("both directions carrying frames")
			break
		}
		select {
		case <-deadline:
			t.Fatalf("no frames after 45s (ana sees peer frame: %v, bo sees peer frame: %v); "+
				"states A=%v B=%v", aOK, bOK, states(meshA), states(meshB))
		case err := <-meshA.Errs():
			t.Fatalf("ana: %v", err)
		case err := <-meshB.Errs():
			t.Fatalf("bo: %v", err)
		case <-time.After(250 * time.Millisecond):
		}
	}

	// The picture that arrived has to be the size the renderer expects, or
	// the sampling arithmetic downstream is working on a lie.
	for _, tl := range meshA.Tiles() {
		if tl.Frame == nil {
			continue
		}
		if tl.Frame.W != video.WireW || tl.Frame.H != video.WireH {
			t.Errorf("received frame is %dx%d, want %dx%d",
				tl.Frame.W, tl.Frame.H, video.WireW, video.WireH)
		}
		if tl.State != call.Live {
			t.Errorf("peer carrying frames is reported as %v", tl.State)
		}
		if tl.Stale {
			t.Error("a frame that just arrived is reported stale")
		}
	}
}

func hasFrame(m *call.Mesh) bool {
	for _, t := range m.Tiles() {
		if t.Frame != nil {
			return true
		}
	}
	return false
}

func states(m *call.Mesh) []string {
	var out []string
	for _, t := range m.Tiles() {
		out = append(out, t.Name+"="+t.State.String())
	}
	if out == nil {
		return []string{"no peers"}
	}
	return out
}

// Leaving must tear the connection down on the other side too, or a call
// keeps drawing a face that has gone.
func TestPeerLeavingIsNoticed(t *testing.T) {
	need(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	code, _ := proto.NewCode()
	ana, err := client.Join(ctx, code, "ana", proto.Video, true)
	if err != nil {
		t.Fatal(err)
	}
	defer ana.Close()
	meshA, err := call.Start(ctx, ana)
	if err != nil {
		t.Fatal(err)
	}
	defer meshA.Close()
	pump(ctx, ana, meshA)

	bo, err := client.Join(ctx, code, "bo", proto.Video, false)
	if err != nil {
		t.Fatal(err)
	}
	meshB, err := call.Start(ctx, bo)
	if err != nil {
		t.Fatal(err)
	}
	pump(ctx, bo, meshB)

	// Wait for ana to know about bo at all.
	deadline := time.After(30 * time.Second)
	for len(meshA.Tiles()) == 0 {
		select {
		case <-deadline:
			t.Fatal("ana never saw bo")
		case <-time.After(100 * time.Millisecond):
		}
	}

	meshB.Close()
	bo.Close()

	deadline = time.After(30 * time.Second)
	for len(meshA.Tiles()) > 0 {
		select {
		case <-deadline:
			t.Fatalf("ana still shows %v after bo left", states(meshA))
		case <-time.After(100 * time.Millisecond):
		}
	}
}
