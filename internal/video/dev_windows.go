//go:build windows

package video

import (
	"context"
	"errors"
	"os/exec"
	"regexp"
	"strings"
)

// DirectShow addresses cameras by name, so the name has to be discovered
// before anything can be opened. Listing is a normal ffmpeg run that always
// exits non-zero -- there is no input to process -- and writes what it found
// to stderr, so the exit status is deliberately ignored.
var dshowVideo = regexp.MustCompile(`"([^"]+)"\s*\((video)\)`)

func listDevices(ctx context.Context, bin string) ([]device, error) {
	cmd := exec.CommandContext(ctx, bin,
		"-hide_banner", "-list_devices", "true", "-f", "dshow", "-i", "dummy")
	out, _ := cmd.CombinedOutput()

	var found []device
	seen := map[string]bool{}
	for _, m := range dshowVideo.FindAllStringSubmatch(string(out), -1) {
		name := strings.TrimSpace(m[1])
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		found = append(found, device{
			name: name,
			args: []string{"-f", "dshow", "-i", "video=" + name},
		})
	}
	return found, nil
}

func defaultDevice(ctx context.Context, bin string) (device, error) {
	found, err := listDevices(ctx, bin)
	if err != nil {
		return device{}, err
	}
	if len(found) == 0 {
		return device{}, errors.New(
			"no camera found.\ncheck that one is connected and that Windows lets apps use it:\n" +
				"Settings › Privacy & security › Camera")
	}
	return found[0], nil
}
