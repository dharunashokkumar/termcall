//go:build linux

package video

import (
	"context"
	"errors"
	"path/filepath"
	"sort"
)

// Video4Linux exposes cameras as device files. The lowest-numbered one is the
// conventional default; higher ones are often the same camera's metadata
// stream rather than a second camera.
func listDevices(ctx context.Context, bin string) ([]device, error) {
	paths, err := filepath.Glob("/dev/video*")
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	found := make([]device, 0, len(paths))
	for _, p := range paths {
		found = append(found, device{
			name: p,
			args: []string{"-f", "v4l2", "-i", p},
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
			"no camera found at /dev/video*.\nif one is plugged in, check that you are in the `video` group")
	}
	return found[0], nil
}
