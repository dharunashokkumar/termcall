//go:build !embedffmpeg

package video

import "errors"

// errNotEmbedded means this build carries no ffmpeg. Development builds do
// not: embedding one costs ~80 MB and a fetch from the network, which is a
// poor trade for `go build ./...` in a loop. Release builds are made with
// -tags embedffmpeg, and CI puts the binary in place first.
var errNotEmbedded = errors.New("this build does not carry ffmpeg")

func unpack() (string, error) { return "", errNotEmbedded }
