// Package call carries video between people, peer to peer.
//
// The Worker is used only to introduce two clients to each other. Once the
// WebRTC handshake completes the frames flow directly between them and
// Cloudflare sees nothing more, which is what keeps a call inside the free
// tier: relaying ten frames a second from four people would spend a quarter
// of the daily request budget on one ten minute call, while the handshake
// costs about twenty messages.
//
// Everyone connects to everyone -- a mesh, not a server. At four people that
// is three connections each, which is exactly why video rooms are capped at
// four: the cost of a mesh climbs as the square, and the fifth person makes
// it worse for all of the others.
package call

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/pion/webrtc/v4"

	"github.com/dharunashokkumar/termcall/internal/client"
	"github.com/dharunashokkumar/termcall/internal/proto"
	"github.com/dharunashokkumar/termcall/internal/video"
)

// signal is one step of a handshake, wrapped so both ends agree what they are
// looking at. The Worker relays this without reading it.
type signal struct {
	Kind      string                     `json:"kind"`
	SDP       *webrtc.SessionDescription `json:"sdp,omitempty"`
	Candidate *webrtc.ICECandidateInit   `json:"cand,omitempty"`
}

// State is how a peer's connection is going, for showing on screen. A call
// that is quietly failing to connect is the worst possible experience, so
// this is deliberately visible rather than internal.
type State int

const (
	Connecting State = iota
	Live
	Failed
	Gone
)

func (s State) String() string {
	switch s {
	case Live:
		return "live"
	case Failed:
		return "failed"
	case Gone:
		return "left"
	default:
		return "connecting"
	}
}

type peer struct {
	id   string
	name string
	pc   *webrtc.PeerConnection

	mu      sync.Mutex
	dc      *webrtc.DataChannel
	state   State
	frame   *video.Frame
	seen    time.Time
	pending []webrtc.ICECandidateInit // arrived before the remote description
	remote  bool                      // remote description is set
}

// Mesh is every connection in one call.
type Mesh struct {
	room *client.Room
	cfg  webrtc.Configuration
	ctx  context.Context

	mu    sync.Mutex
	peers map[string]*peer

	enc  video.Encoder
	errs chan error
}

// Start brings up the mesh and keeps it in step with the room.
//
// The rule for who calls whom is simply: you offer to everyone who was
// already here when you arrived. It needs no negotiation and no tie-break,
// because for any two people exactly one of them arrived second.
func Start(ctx context.Context, room *client.Room) (*Mesh, error) {
	cfg, err := iceConfig(ctx)
	if err != nil {
		return nil, err
	}
	m := &Mesh{
		room:  room,
		cfg:   cfg,
		ctx:   ctx,
		peers: map[string]*peer{},
		errs:  make(chan error, 8),
	}
	for _, p := range room.Peers() {
		if err := m.offer(p); err != nil {
			m.note(fmt.Errorf("calling %s: %w", p.Name, err))
		}
	}
	return m, nil
}

// Handle folds one room event into the mesh. The caller owns the event loop,
// so that the screen sees joins and parts at the same moment the mesh does.
func (m *Mesh) Handle(e proto.Event) {
	switch e.T {
	case "join":
		// They arrived second, so they will offer to us. Nothing to do but
		// be ready; the connection appears when their offer lands.
	case "part":
		m.drop(e.Who.ID)
	case "sig":
		if err := m.onSignal(e); err != nil {
			m.note(err)
		}
	}
}

func (m *Mesh) note(err error) {
	select {
	case m.errs <- err:
	default:
	}
}

// Errs reports handshake trouble. It is drained by the screen and shown, not
// logged: on a terminal there is nowhere else for it to go.
func (m *Mesh) Errs() <-chan error { return m.errs }

func (m *Mesh) get(id string) *peer {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.peers[id]
}

// Tile is one person's latest picture, for drawing.
type Tile struct {
	ID    string
	Name  string
	State State
	Frame *video.Frame
	Stale bool
}

// Tiles is what to draw right now, in a stable order so faces do not swap
// places between frames.
func (m *Mesh) Tiles() []Tile {
	m.mu.Lock()
	ps := make([]*peer, 0, len(m.peers))
	for _, p := range m.peers {
		ps = append(ps, p)
	}
	m.mu.Unlock()

	out := make([]Tile, 0, len(ps))
	for _, p := range ps {
		p.mu.Lock()
		t := Tile{ID: p.id, Name: p.name, State: p.state, Frame: p.frame}
		// A peer whose connection is up but whose frames stopped should not
		// keep showing a frozen face as though it were live.
		t.Stale = p.frame != nil && time.Since(p.seen) > 2*time.Second
		p.mu.Unlock()
		out = append(out, t)
	}
	// Sorted by id: any stable order will do, so long as it is the same on
	// every frame and does not depend on map iteration.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].ID < out[j-1].ID; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// Send encodes one frame and pushes it to everyone.
//
// The encode happens once, not once per peer, and a peer whose channel is not
// open or is backed up is skipped rather than waited for. Video is only worth
// sending while it is current.
func (m *Mesh) Send(f *video.Frame) {
	b, err := m.enc.Encode(f)
	if err != nil {
		m.note(err)
		return
	}
	m.mu.Lock()
	ps := make([]*peer, 0, len(m.peers))
	for _, p := range m.peers {
		ps = append(ps, p)
	}
	m.mu.Unlock()

	for _, p := range ps {
		p.mu.Lock()
		dc := p.dc
		p.mu.Unlock()
		if dc == nil || dc.ReadyState() != webrtc.DataChannelStateOpen {
			continue
		}
		// Dropping when the buffer is deep is the point of an unreliable
		// channel: a backlog of stale frames helps nobody, and sending into
		// one only makes the delay permanent.
		if dc.BufferedAmount() > 1<<20 {
			continue
		}
		if err := dc.Send(b); err != nil {
			m.note(err)
		}
	}
}

func (m *Mesh) Close() {
	m.mu.Lock()
	ps := m.peers
	m.peers = map[string]*peer{}
	m.mu.Unlock()
	for _, p := range ps {
		p.pc.Close()
	}
}
