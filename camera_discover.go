package cameracoordinator

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type CameraDiscovery interface {
	// Discover returns a map of video device paths (e.g. "video0") to their info.
	Discover() (map[string]VideoDeviceInfo, error)
}

type CameraDiscoveryV4L2 struct{}

func (d *CameraDiscoveryV4L2) Discover() (map[string]VideoDeviceInfo, error) {
	const (
		v4l2CapVideoCapture = 0x00000001
		v4l2CapDeviceCaps   = 0x80000000
	)

	results := map[string]VideoDeviceInfo{}
	seenCanonical := map[string]struct{}{}

	candidates := []string{}

	// Collect /dev/v4l/by-id entries if available.
	for _, byIDPath := range []string{"/dev/v4l/by-id"} {
		entries, err := os.ReadDir(byIDPath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("read %s: %w", byIDPath, err)
		}

		for _, entry := range entries {
			candidate := filepath.Join(byIDPath, entry.Name())
			if resolved, err := filepath.EvalSymlinks(candidate); err == nil {
				candidate = resolved
			}
			if strings.HasPrefix(filepath.Base(candidate), "video") {
				candidates = append(candidates, candidate)
			}
		}
	}

	// Collect /dev/video* device nodes.
	devEntries, err := os.ReadDir("/dev")
	if err != nil {
		return nil, fmt.Errorf("read /dev: %w", err)
	}
	for _, entry := range devEntries {
		if strings.HasPrefix(entry.Name(), "video") {
			candidates = append(candidates, filepath.Join("/dev", entry.Name()))
		}
	}

	// Stable ordering for predictable logs and behavior.
	sort.Strings(candidates)

	fmt.Println(candidates)

	for _, candidate := range candidates {
		canonical := candidate
		if resolved, err := filepath.EvalSymlinks(candidate); err == nil {
			canonical = resolved
		}

		if _, exists := seenCanonical[canonical]; exists {
			continue
		}

		info, err := QueryV4L2Device(canonical)
		if err != nil {
			continue
		}

		effectiveCaps := info.Capabilities
		if (info.Capabilities & v4l2CapDeviceCaps) != 0 {
			effectiveCaps = info.DeviceCaps
		}
		if (effectiveCaps & v4l2CapVideoCapture) == 0 {
			continue
		}

		results[filepath.Base(canonical)] = info
		seenCanonical[canonical] = struct{}{}
	}

	return results, nil

}
