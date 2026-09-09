//go:build darwin

package video

import (
	"context"
	"errors"
	"os/exec"
	"regexp"
	"strings"
)

// AVFoundation addresses cameras by index, and lists them on stderr under a
// video heading followed by an audio one. Only the video half is wanted, so
// parsing stops at the audio heading.
var avDevice = regexp.MustCompile(`\[(\d+)\]\s+(.+)`)

func listDevices(ctx context.Context, bin string) ([]device, error) {
	cmd := exec.CommandContext(ctx, bin,
		"-hide_banner", "-f", "avfoundation", "-list_devices", "true", "-i", "")
	out, _ := cmd.CombinedOutput()

	var found []device
	video := false
	for _, line := range strings.Split(string(out), "\n") {
		switch {
		case strings.Contains(line, "AVFoundation video devices"):
			video = true
			continue
		case strings.Contains(line, "AVFoundation audio devices"):
			video = false
			continue
		}
		if !video {
			continue
		}
		if m := avDevice.FindStringSubmatch(line); m != nil {
			found = append(found, device{
				name: strings.TrimSpace(m[2]),
				// -framerate must precede -i: it configures the device being
				// opened, not the output.
				args: []string{"-f", "avfoundation", "-framerate", "30", "-i", m[1]},
			})
		}
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
			"no camera found.\ncheck that your terminal is allowed to use one:\n" +
				"System Settings › Privacy & Security › Camera")
	}
	return found[0], nil
}
