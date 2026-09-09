// Package client is one connection to one room.
//
// It is used for both kinds of room. For chat that is the whole story: every
// message in the room arrives on Events. For video it carries only the WebRTC
// handshake, and the frames themselves go peer to peer -- see internal/video.
package client

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/dharunashokkumar/termcall/internal/proto"
)

// DefaultHost is where the Worker lives. TC_HOST overrides it, which is how
// `wrangler dev` gets tested: TC_HOST=http://127.0.0.1:8787.
const DefaultHost = "call.dharun.dev"

func Host() string {
	if h := strings.TrimSpace(os.Getenv("TC_HOST")); h != "" {
		return h
	}
	return DefaultHost
}

// dialHTTP is the client every handshake goes through. It exists to pin
// HTTP/1.1.
//
// A WebSocket upgrade is an HTTP/1.1 mechanism: it works by asking the server
// to switch protocols on the connection. Go's default transport negotiates
// HTTP/2 with Cloudflare over TLS, where the Upgrade header is not permitted,
// and the handshake then hangs until the dial deadline rather than failing
// with anything that explains itself. A non-nil, empty TLSNextProto is what
// tells net/http never to negotiate h2.
var dialHTTP = &http.Client{
	Transport: &http.Transport{
		Proxy:             http.ProxyFromEnvironment,
		ForceAttemptHTTP2: false,
		TLSNextProto:      map[string]func(string, *tls.Conn) http.RoundTripper{},
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout: 10 * time.Second,
	},
}

var (
	ErrTaken     = errors.New("that room code is already in use")
	ErrNoRoom    = errors.New("no room with that code")
	ErrFull      = errors.New("that room is full")
	ErrWrongMode = errors.New("wrong kind of room for that code")
)

type Room struct {
	Code string
	Mode proto.Mode
	Me   proto.Peer
	Cap  int

	conn   *websocket.Conn
	cancel context.CancelFunc
	events chan proto.Event

	mu    sync.Mutex
	peers map[string]proto.Peer

	writeMu sync.Mutex
	closing sync.Once
	done    chan struct{}
	err     error
}

// endpoint turns a host -- bare, or with any scheme -- into a websocket URL.
// Plain http means local development, and has to stay ws:// rather than wss://.
func endpoint(host, code string, q url.Values) string {
	scheme := "wss"
	switch {
	case strings.HasPrefix(host, "http://"):
		scheme, host = "ws", strings.TrimPrefix(host, "http://")
	case strings.HasPrefix(host, "https://"):
		scheme, host = "wss", strings.TrimPrefix(host, "https://")
	case strings.HasPrefix(host, "ws://"):
		scheme, host = "ws", strings.TrimPrefix(host, "ws://")
	case strings.HasPrefix(host, "wss://"):
		scheme, host = "wss", strings.TrimPrefix(host, "wss://")
	}
	u := url.URL{Scheme: scheme, Host: strings.TrimSuffix(host, "/"),
		Path: "/r/" + code, RawQuery: q.Encode()}
	return u.String()
}

// Join opens a room. With create true the server refuses a code that is
// already live; with it false the server refuses a code that is not.
//
// The welcome frame is read here, before returning, so a caller always has a
// populated roster and never has to handle a room it does not yet know the
// shape of.
func Join(ctx context.Context, code, name string, mode proto.Mode, create bool) (*Room, error) {
	q := url.Values{"name": {name}, "mode": {string(mode)}}
	if create {
		q.Set("create", "1")
	}

	dialCtx, cancelDial := context.WithTimeout(ctx, 15*time.Second)
	defer cancelDial()

	conn, resp, err := websocket.Dial(dialCtx, endpoint(Host(), code, q),
		&websocket.DialOptions{HTTPClient: dialHTTP})
	if err != nil {
		return nil, dialError(resp, err)
	}
	// Chat lines are capped server-side; the headroom is for the welcome
	// frame in a full room and for signalling blobs, which are the largest
	// thing this socket ever carries.
	conn.SetReadLimit(64 << 10)

	runCtx, cancel := context.WithCancel(ctx)
	r := &Room{
		Code:   code,
		Mode:   mode,
		conn:   conn,
		cancel: cancel,
		events: make(chan proto.Event, 128),
		peers:  map[string]proto.Peer{},
		done:   make(chan struct{}),
	}

	welcome, err := r.read(dialCtx)
	if err != nil {
		cancel()
		conn.Close(websocket.StatusInternalError, "no welcome")
		return nil, fmt.Errorf("joining %s: %w", code, err)
	}
	if welcome.T != "welcome" {
		cancel()
		conn.Close(websocket.StatusProtocolError, "bad welcome")
		return nil, fmt.Errorf("joining %s: server sent %q first", code, welcome.T)
	}
	r.Me, r.Cap, r.Mode = welcome.You, welcome.Cap, welcome.Mode
	for _, p := range welcome.Peers {
		r.peers[p.ID] = p
	}

	go r.pump(runCtx)
	go r.keepalive(runCtx)
	return r, nil
}

// dialError turns a failed handshake into something worth showing a person.
// The reason comes from a response header rather than the body: a rejected
// WebSocket upgrade does not reliably deliver its body to the client, but
// headers always arrive.
func dialError(resp *http.Response, err error) error {
	if resp == nil {
		return fmt.Errorf("cannot reach %s (%v)", Host(), err)
	}
	switch resp.Header.Get("x-termcall-error") {
	case "taken":
		return ErrTaken
	case "notfound":
		return ErrNoRoom
	case "full":
		return ErrFull
	case "mode":
		return ErrWrongMode
	}
	return fmt.Errorf("server said %s", resp.Status)
}

func (r *Room) read(ctx context.Context) (proto.Event, error) {
	var e proto.Event
	_, data, err := r.conn.Read(ctx)
	if err != nil {
		return e, err
	}
	if err := json.Unmarshal(data, &e); err != nil {
		return e, fmt.Errorf("unreadable message: %w", err)
	}
	return e, nil
}

// pump is the only reader. It keeps the roster current on the way past, so
// callers can ask Peers() without tracking joins and parts themselves.
func (r *Room) pump(ctx context.Context) {
	defer close(r.events)
	defer close(r.done)
	for {
		e, err := r.read(ctx)
		if err != nil {
			if ctx.Err() == nil {
				r.err = err
			}
			return
		}
		switch e.T {
		case "join":
			r.mu.Lock()
			r.peers[e.Who.ID] = e.Who
			r.mu.Unlock()
		case "part":
			r.mu.Lock()
			delete(r.peers, e.Who.ID)
			r.mu.Unlock()
		}
		select {
		case r.events <- e:
		case <-ctx.Done():
			return
		}
	}
}

// keepalive uses protocol-level pings, not application messages. With the
// hibernation API the runtime answers these at the edge without waking the
// Durable Object, so an idle room costs nothing to hold open -- an app-level
// ping would bill a request every time.
func (r *Room) keepalive(ctx context.Context) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-r.done:
			return
		case <-t.C:
			c, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := r.conn.Ping(c)
			cancel()
			if err != nil {
				return
			}
		}
	}
}

func (r *Room) Events() <-chan proto.Event { return r.events }

// Err reports why the connection ended, or nil if we closed it ourselves.
func (r *Room) Err() error { return r.err }

func (r *Room) Peers() []proto.Peer {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]proto.Peer, 0, len(r.peers))
	for _, p := range r.peers {
		out = append(out, p)
	}
	return out
}

func (r *Room) send(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return r.conn.Write(ctx, websocket.MessageText, b)
}

func (r *Room) Say(text string) error {
	if len(text) > proto.TextMax {
		text = text[:proto.TextMax]
	}
	return r.send(proto.Say{T: "say", Text: text})
}

// Sig relays one WebRTC blob to one peer. The server does not read it.
func (r *Room) Sig(to string, data json.RawMessage) error {
	return r.send(proto.Sig{T: "sig", To: to, Data: data})
}

func (r *Room) Close() {
	r.closing.Do(func() {
		r.conn.Close(websocket.StatusNormalClosure, "bye")
		r.cancel()
	})
}

// HTTPBase is the server's plain-HTTP base URL, for the few things that are
// not the WebSocket: currently just /ice.
func HTTPBase() string {
	h := Host()
	switch {
	case strings.HasPrefix(h, "http://"), strings.HasPrefix(h, "https://"):
		return strings.TrimSuffix(h, "/")
	case strings.HasPrefix(h, "ws://"):
		return "http://" + strings.TrimSuffix(strings.TrimPrefix(h, "ws://"), "/")
	case strings.HasPrefix(h, "wss://"):
		return "https://" + strings.TrimSuffix(strings.TrimPrefix(h, "wss://"), "/")
	}
	return "https://" + strings.TrimSuffix(h, "/")
}

// Name returns the display name of a peer we know about, or the id if the
// roster has already forgotten them.
func (r *Room) Name(id string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if p, ok := r.peers[id]; ok {
		return p.Name
	}
	return id
}
