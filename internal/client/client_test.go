package client_test

// Integration tests against a running server. They are skipped unless TC_HOST
// points at one:
//
//	cd worker && npx wrangler dev --port 8787 --local
//	TC_HOST=http://127.0.0.1:8787 go test ./internal/client/
//
// There is no mock. The things worth testing here -- that create refuses a
// live code, that join refuses a dead one, that a room fills up -- are all
// decisions the Durable Object makes, so testing them against anything but
// the real one would only prove the mock agrees with itself.

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/dharunashokkumar/termcall/internal/client"
	"github.com/dharunashokkumar/termcall/internal/proto"
)

func host(t *testing.T) {
	t.Helper()
	if os.Getenv("TC_HOST") == "" {
		t.Skip("set TC_HOST to a running termcall server to run these")
	}
}

func ctx(t *testing.T) context.Context {
	c, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return c
}

func code(t *testing.T) string {
	t.Helper()
	c, err := proto.NewCode()
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// await waits for the next event of a given type, failing if it does not come.
func await(t *testing.T, r *client.Room, kind string) proto.Event {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case e, ok := <-r.Events():
			if !ok {
				t.Fatalf("connection closed waiting for %q (%v)", kind, r.Err())
			}
			if e.T == kind {
				return e
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %q", kind)
		}
	}
}

func TestChatRoundTrip(t *testing.T) {
	host(t)
	c := code(t)

	a, err := client.Join(ctx(t), c, "ana", proto.Chat, true)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer a.Close()
	if a.Me.Name != "ana" || a.Cap != proto.ChatMax {
		t.Fatalf("welcome said name=%q cap=%d", a.Me.Name, a.Cap)
	}

	b, err := client.Join(ctx(t), c, "bo", proto.Chat, false)
	if err != nil {
		t.Fatalf("join: %v", err)
	}
	defer b.Close()

	// The creator learns about the arrival.
	if e := await(t, a, "join"); e.Who.Name != "bo" {
		t.Fatalf("join event named %q", e.Who.Name)
	}
	if got := len(a.Peers()); got != 1 {
		t.Fatalf("roster has %d others, want 1", got)
	}

	// A message reaches both people, sender included: the screen shows what
	// the room broadcast, not what was typed, so the order matches everywhere.
	if err := b.Say("hello there"); err != nil {
		t.Fatal(err)
	}
	for name, r := range map[string]*client.Room{"ana": a, "bo": b} {
		e := await(t, r, "msg")
		if e.From != "bo" || e.Text != "hello there" {
			t.Fatalf("%s saw from=%q text=%q", name, e.From, e.Text)
		}
		if e.TS == 0 {
			t.Fatalf("%s saw a message with no timestamp", name)
		}
	}

	b.Close()
	if e := await(t, a, "part"); e.Who.Name != "bo" {
		t.Fatalf("part event named %q", e.Who.Name)
	}
}

func TestDuplicateNamesAreSeparated(t *testing.T) {
	host(t)
	c := code(t)
	a, err := client.Join(ctx(t), c, "sam", proto.Chat, true)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	b, err := client.Join(ctx(t), c, "sam", proto.Chat, false)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	if b.Me.Name == a.Me.Name {
		t.Fatalf("both people are called %q", b.Me.Name)
	}
	if b.Me.Name != "sam2" {
		t.Fatalf("second sam became %q, want sam2", b.Me.Name)
	}
}

func TestJoinUnknownCode(t *testing.T) {
	host(t)
	_, err := client.Join(ctx(t), code(t), "ana", proto.Chat, false)
	if !errors.Is(err, client.ErrNoRoom) {
		t.Fatalf("got %v, want ErrNoRoom", err)
	}
}

func TestCreateTwice(t *testing.T) {
	host(t)
	c := code(t)
	a, err := client.Join(ctx(t), c, "ana", proto.Chat, true)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	if _, err := client.Join(ctx(t), c, "bo", proto.Chat, true); !errors.Is(err, client.ErrTaken) {
		t.Fatalf("got %v, want ErrTaken", err)
	}
}

func TestWrongMode(t *testing.T) {
	host(t)
	c := code(t)
	a, err := client.Join(ctx(t), c, "ana", proto.Chat, true)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	if _, err := client.Join(ctx(t), c, "bo", proto.Video, false); !errors.Is(err, client.ErrWrongMode) {
		t.Fatalf("got %v, want ErrWrongMode", err)
	}
}

// A video call is a peer-to-peer mesh, so its cap is what stops the fifth
// person rather than a preference. Chat has the same machinery at a higher
// number; testing the tighter one is cheaper and exercises the same path.
func TestVideoRoomFills(t *testing.T) {
	host(t)
	c := code(t)
	names := []string{"ana", "bo", "cy", "di"}
	for i, n := range names {
		r, err := client.Join(ctx(t), c, n, proto.Video, i == 0)
		if err != nil {
			t.Fatalf("%s could not get in: %v", n, err)
		}
		defer r.Close()
		if r.Cap != proto.VideoMax {
			t.Fatalf("cap is %d, want %d", r.Cap, proto.VideoMax)
		}
	}
	if _, err := client.Join(ctx(t), c, "ed", proto.Video, false); !errors.Is(err, client.ErrFull) {
		t.Fatalf("got %v, want ErrFull", err)
	}
}

// Signalling is what video needs from the server, and all it needs: an opaque
// blob delivered to exactly one named peer.
func TestSignallingReachesOnePeer(t *testing.T) {
	host(t)
	c := code(t)
	a, err := client.Join(ctx(t), c, "ana", proto.Video, true)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := client.Join(ctx(t), c, "bo", proto.Video, false)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	await(t, a, "join")

	if err := a.Sig(b.Me.ID, []byte(`{"kind":"offer"}`)); err != nil {
		t.Fatal(err)
	}
	e := await(t, b, "sig")
	if e.Peer != a.Me.ID {
		t.Fatalf("sig came from %q, want %q", e.Peer, a.Me.ID)
	}
	if string(e.Data) != `{"kind":"offer"}` {
		t.Fatalf("sig body was %q", e.Data)
	}
}

// Leaving must free the code. Without this a room code, once used, would stay
// claimed for the life of the account -- the Durable Object holding the "this
// room exists" marker never goes away on its own.
func TestEmptyRoomFreesItsCode(t *testing.T) {
	host(t)
	c := code(t)

	a, err := client.Join(ctx(t), c, "ana", proto.Chat, true)
	if err != nil {
		t.Fatal(err)
	}
	a.Close()

	// The close handler runs after the socket goes; give it a moment before
	// asking whether the room is gone.
	var last error
	for i := 0; i < 20; i++ {
		time.Sleep(100 * time.Millisecond)
		b, err := client.Join(ctx(t), c, "bo", proto.Chat, true)
		if err == nil {
			b.Close()
			return
		}
		last = err
	}
	t.Fatalf("code %s never came free: %v", c, last)
}

// Closing must not block. The polite WebSocket close waits for the server to
// echo a close frame; if the server never does, every exit from every room
// stalls for the library's timeout.
func TestCloseIsPrompt(t *testing.T) {
	host(t)
	r, err := client.Join(ctx(t), code(t), "ana", proto.Chat, true)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	r.Close()
	if took := time.Since(start); took > time.Second {
		t.Fatalf("Close took %v; the server is not completing the close handshake", took)
	}
}
