// Package proto is the contract between the Go client and the Cloudflare
// Worker. Every constant here is mirrored in worker/src/limits.js; change the
// two together or rooms will disagree about what fits in them.
package proto

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"strings"
)

const (
	ChatMax  = 10
	VideoMax = 4
	TextMax  = 2000
	NameMax  = 24

	// No 0/O/1/I/L, so a code read down a phone line survives the trip.
	CodeAlphabet = "23456789ABCDEFGHJKMNPQRSTUVWXYZ"
	CodeLen      = 6
)

// Mode is what a room is for. It is fixed when the room is created; joining
// with the wrong one is refused rather than silently converted, because a
// video client and a chat client draw entirely different screens.
type Mode string

const (
	Chat  Mode = "chat"
	Video Mode = "video"
)

func (m Mode) Cap() int {
	if m == Video {
		return VideoMax
	}
	return ChatMax
}

// Peer is one person in a room. Name is what humans see and is unique within
// the room; ID is what signalling addresses and is never shown.
type Peer struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Event is anything the server sends. The T field says which of the rest are
// populated:
//
//	welcome  You, Mode, Cap, Peers   sent once, before anything else
//	join     Who                     someone arrived
//	part     Who                     someone left
//	msg      From, Text, TS          a chat line; From is a display name
//	sig      Peer, Data              a WebRTC handshake blob; Peer is an ID
//	pong     -                       keepalive answer
type Event struct {
	T     string          `json:"t"`
	You   Peer            `json:"you"`
	Mode  Mode            `json:"mode"`
	Cap   int             `json:"cap"`
	Peers []Peer          `json:"peers"`
	Who   Peer            `json:"who"`
	From  string          `json:"from"`
	Text  string          `json:"text"`
	TS    int64           `json:"ts"`
	Peer  string          `json:"peer"`
	Data  json.RawMessage `json:"data"`
}

// Say is a chat line on its way out.
type Say struct {
	T    string `json:"t"`
	Text string `json:"text"`
}

// Sig is a WebRTC offer, answer or ICE candidate addressed to one peer. The
// Worker relays it without looking inside.
type Sig struct {
	T    string          `json:"t"`
	To   string          `json:"to"`
	Data json.RawMessage `json:"data"`
}

// NewCode returns a fresh room code. Collisions are left to the server: with
// 31^6 codes a clash is rare enough that retrying on the 409 costs less than
// a round trip to reserve one would.
func NewCode() (string, error) {
	b := make([]byte, CodeLen)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	out := make([]byte, CodeLen)
	for i, v := range b {
		out[i] = CodeAlphabet[int(v)%len(CodeAlphabet)]
	}
	return string(out), nil
}

var ErrBadCode = errors.New("a room code is 6 characters, letters and digits")

// CleanCode accepts what a person actually types -- lowercase, stray spaces,
// the dashes people add to break up a code -- and returns the canonical form.
func CleanCode(s string) (string, error) {
	s = strings.ToUpper(strings.TrimSpace(s))
	s = strings.NewReplacer("-", "", " ", "", "_", "").Replace(s)
	if len(s) != CodeLen {
		return "", ErrBadCode
	}
	for _, c := range s {
		if !strings.ContainsRune(CodeAlphabet, c) {
			return "", ErrBadCode
		}
	}
	return s, nil
}

var ErrBadName = errors.New("a name is required")

// CleanName trims a typed name to something safe to put on a screen: no
// control characters, no leading or trailing space, and short enough that a
// roster still fits across a terminal.
func CleanName(s string) (string, error) {
	s = strings.TrimSpace(s)
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
	if s == "" {
		return "", ErrBadName
	}
	if len(s) > NameMax {
		s = strings.TrimSpace(s[:NameMax])
	}
	return s, nil
}
