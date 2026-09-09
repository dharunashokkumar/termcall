package call

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/pion/webrtc/v4"

	"github.com/dharunashokkumar/termcall/internal/client"
	"github.com/dharunashokkumar/termcall/internal/proto"
	"github.com/dharunashokkumar/termcall/internal/video"
)

// iceConfig asks the Worker how to get through the network.
//
// The list is served rather than compiled in so that a TURN relay can be
// added later -- by setting three secrets on the Worker -- without every
// installed client needing an update.
func iceConfig(ctx context.Context) (webrtc.Configuration, error) {
	var cfg webrtc.Configuration

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", client.HTTPBase()+"/ice", nil)
	if err != nil {
		return cfg, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return cfg, fmt.Errorf("asking %s how to connect: %w", client.Host(), err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return cfg, fmt.Errorf("asking %s how to connect: %s", client.Host(), resp.Status)
	}

	var body struct {
		ICEServers []webrtc.ICEServer `json:"iceServers"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return cfg, err
	}
	cfg.ICEServers = body.ICEServers
	return cfg, nil
}

func (m *Mesh) send(to string, s signal) error {
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return m.room.Sig(to, b)
}

func (m *Mesh) newPeer(id, name string) (*peer, error) {
	pc, err := webrtc.NewPeerConnection(m.cfg)
	if err != nil {
		return nil, err
	}
	p := &peer{id: id, name: name, pc: pc, state: Connecting}

	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return // gathering finished; there is nothing to send
		}
		init := c.ToJSON()
		if err := m.send(id, signal{Kind: "candidate", Candidate: &init}); err != nil {
			m.note(err)
		}
	})

	pc.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		p.mu.Lock()
		switch s {
		case webrtc.PeerConnectionStateConnected:
			p.state = Live
		case webrtc.PeerConnectionStateFailed:
			p.state = Failed
		case webrtc.PeerConnectionStateClosed:
			p.state = Gone
		case webrtc.PeerConnectionStateDisconnected:
			// Disconnected is often temporary -- a changed network, a lost
			// packet run -- and ICE may recover on its own, so this is not
			// reported as failure.
			p.state = Connecting
		}
		p.mu.Unlock()
	})

	// The side that answers does not create the channel; it is handed one.
	pc.OnDataChannel(func(dc *webrtc.DataChannel) { m.bind(p, dc) })

	m.mu.Lock()
	m.peers[id] = p
	m.mu.Unlock()
	return p, nil
}

// bind attaches the frame channel, whichever side made it.
func (m *Mesh) bind(p *peer, dc *webrtc.DataChannel) {
	dc.OnOpen(func() {
		p.mu.Lock()
		p.dc = dc
		p.mu.Unlock()
	})
	dc.OnClose(func() {
		p.mu.Lock()
		p.dc = nil
		p.mu.Unlock()
	})
	dc.OnMessage(func(msg webrtc.DataChannelMessage) {
		f, err := video.Decode(msg.Data)
		if err != nil {
			// Expected, and not worth reporting. The channel is unreliable
			// by design, so a partial or reordered frame now and then is
			// normal; the next one is 100ms away.
			return
		}
		p.mu.Lock()
		p.frame, p.seen = f, time.Now()
		p.mu.Unlock()
	})
}

// offer starts a connection to someone who was already in the room.
func (m *Mesh) offer(who proto.Peer) error {
	p, err := m.newPeer(who.ID, who.Name)
	if err != nil {
		return err
	}

	// Unordered and never retransmitted. Video is only worth having while it
	// is current: a frame that arrives late is worse than one that never
	// arrives, because waiting for it delays every frame behind it.
	ordered := false
	var retransmits uint16 = 0
	dc, err := p.pc.CreateDataChannel("frames", &webrtc.DataChannelInit{
		Ordered:        &ordered,
		MaxRetransmits: &retransmits,
	})
	if err != nil {
		return err
	}
	m.bind(p, dc)

	sdp, err := p.pc.CreateOffer(nil)
	if err != nil {
		return err
	}
	if err := p.pc.SetLocalDescription(sdp); err != nil {
		return err
	}
	return m.send(who.ID, signal{Kind: "offer", SDP: &sdp})
}

func (m *Mesh) onSignal(e proto.Event) error {
	var s signal
	if err := json.Unmarshal(e.Data, &s); err != nil {
		return fmt.Errorf("unreadable signal from %s: %w", e.Peer, err)
	}

	switch s.Kind {
	case "offer":
		if s.SDP == nil {
			return fmt.Errorf("offer from %s carried no description", e.Peer)
		}
		p := m.get(e.Peer)
		if p == nil {
			var err error
			if p, err = m.newPeer(e.Peer, m.room.Name(e.Peer)); err != nil {
				return err
			}
		}
		if err := p.setRemote(*s.SDP); err != nil {
			return err
		}
		sdp, err := p.pc.CreateAnswer(nil)
		if err != nil {
			return err
		}
		if err := p.pc.SetLocalDescription(sdp); err != nil {
			return err
		}
		return m.send(e.Peer, signal{Kind: "answer", SDP: &sdp})

	case "answer":
		if s.SDP == nil {
			return fmt.Errorf("answer from %s carried no description", e.Peer)
		}
		p := m.get(e.Peer)
		if p == nil {
			return fmt.Errorf("answer from %s, who we never called", e.Peer)
		}
		return p.setRemote(*s.SDP)

	case "candidate":
		if s.Candidate == nil {
			return nil
		}
		p := m.get(e.Peer)
		if p == nil {
			// Candidates can outrun the offer they belong to. There is
			// nothing to hold them against yet, and the offer will bring
			// its own.
			return nil
		}
		return p.addCandidate(*s.Candidate)
	}
	return nil
}

// setRemote applies a description and releases any candidates that arrived
// before it. Trickle ICE means candidates and descriptions race, and adding a
// candidate before the description it belongs to is an error.
func (p *peer) setRemote(d webrtc.SessionDescription) error {
	if err := p.pc.SetRemoteDescription(d); err != nil {
		return err
	}
	p.mu.Lock()
	p.remote = true
	held := p.pending
	p.pending = nil
	p.mu.Unlock()

	for _, c := range held {
		if err := p.pc.AddICECandidate(c); err != nil {
			return err
		}
	}
	return nil
}

func (p *peer) addCandidate(c webrtc.ICECandidateInit) error {
	p.mu.Lock()
	if !p.remote {
		p.pending = append(p.pending, c)
		p.mu.Unlock()
		return nil
	}
	p.mu.Unlock()
	return p.pc.AddICECandidate(c)
}

func (m *Mesh) drop(id string) {
	m.mu.Lock()
	p := m.peers[id]
	delete(m.peers, id)
	m.mu.Unlock()
	if p != nil {
		p.pc.Close()
	}
}
