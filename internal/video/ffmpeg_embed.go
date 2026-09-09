//go:build embedffmpeg

package video

// Built with -tags embedffmpeg, which requires ffmpeg.bin.gz to exist in this
// directory. .github/workflows/release.yml downloads a static ffmpeg for the
// target platform and puts it there. See tools/fetch-ffmpeg.sh to do the same
// by hand.

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

//go:embed ffmpeg.bin.gz
var packed []byte

var errNotEmbedded = errors.New("this build does not carry ffmpeg")

// unpack writes the built-in ffmpeg to the cache directory, once, and returns
// where it landed.
//
// The file is named for the hash of what it contains, so a termcall carrying a
// different ffmpeg writes a different file rather than fighting over one --
// two versions can sit side by side, and an upgrade never has to decide
// whether the copy already there is stale.
func unpack() (string, error) {
	if len(packed) == 0 {
		return "", errNotEmbedded
	}
	sum := sha256.Sum256(packed)
	dir, err := cacheDir()
	if err != nil {
		return "", err
	}
	dst := filepath.Join(dir, exeName("ffmpeg-"+hex.EncodeToString(sum[:8])))

	if runnable(dst) {
		return dst, nil
	}

	// Write beside the target and rename into place. Two termcalls started at
	// once would otherwise each see a half-written file and try to run it.
	tmp, err := os.CreateTemp(dir, "ffmpeg-*.part")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())

	zr, err := gzip.NewReader(bytes.NewReader(packed))
	if err != nil {
		tmp.Close()
		return "", err
	}
	if _, err := io.Copy(tmp, zr); err != nil {
		tmp.Close()
		return "", fmt.Errorf("unpacking ffmpeg: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Chmod(tmp.Name(), 0o755); err != nil {
		return "", err
	}
	if err := os.Rename(tmp.Name(), dst); err != nil {
		// Losing the race is fine: whoever won wrote the same bytes, because
		// the name is the hash of them.
		if runnable(dst) {
			return dst, nil
		}
		return "", err
	}
	return dst, nil
}
