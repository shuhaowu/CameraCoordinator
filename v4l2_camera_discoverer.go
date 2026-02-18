package cameracoordinator

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"structs"
	"unsafe"

	"golang.org/x/sys/unix"
)

type V4L2CapabilityCapabilities uint32

const VIDIOC_QUERYCAP = 0x80685600

const (
	V4L2CapVideoCapture       V4L2CapabilityCapabilities = 0x00000001
	V4L2CapVideoCaptureMplane V4L2CapabilityCapabilities = 0x00001000
	V4L2CapVideoOutput        V4L2CapabilityCapabilities = 0x00000002
	V4L2CapVideoOutputMplane  V4L2CapabilityCapabilities = 0x00002000
	V4L2CapVideoM2M           V4L2CapabilityCapabilities = 0x00008000
	V4L2CapVideoM2MMplane     V4L2CapabilityCapabilities = 0x00004000
	V4L2CapVideoOverlay       V4L2CapabilityCapabilities = 0x00000004
	V4L2CapVbiCapture         V4L2CapabilityCapabilities = 0x00000010
	V4L2CapVbiOutput          V4L2CapabilityCapabilities = 0x00000020
	V4L2CapSlicedVbiCapture   V4L2CapabilityCapabilities = 0x00000040
	V4L2CapSlicedVbiOutput    V4L2CapabilityCapabilities = 0x00000080
	V4L2CapRdsCapture         V4L2CapabilityCapabilities = 0x00000100
	V4L2CapVideoOutputOverlay V4L2CapabilityCapabilities = 0x00000200
	V4L2CapHwFreqSeek         V4L2CapabilityCapabilities = 0x00000400
	V4L2CapRdsOutput          V4L2CapabilityCapabilities = 0x00000800
	V4L2CapTuner              V4L2CapabilityCapabilities = 0x00010000
	V4L2CapAudio              V4L2CapabilityCapabilities = 0x00020000
	V4L2CapRadio              V4L2CapabilityCapabilities = 0x00040000
	V4L2CapModulator          V4L2CapabilityCapabilities = 0x00080000
	V4L2CapSdrCapture         V4L2CapabilityCapabilities = 0x00100000
	V4L2CapExtPixFormat       V4L2CapabilityCapabilities = 0x00200000
	V4L2CapSdrOutput          V4L2CapabilityCapabilities = 0x00400000
	V4L2CapMetaCapture        V4L2CapabilityCapabilities = 0x00800000
	V4L2CapReadwrite          V4L2CapabilityCapabilities = 0x01000000
	V4L2CapEdid               V4L2CapabilityCapabilities = 0x02000000
	V4L2CapStreaming          V4L2CapabilityCapabilities = 0x04000000
	V4L2CapMetaOutput         V4L2CapabilityCapabilities = 0x08000000
	V4L2CapTouch              V4L2CapabilityCapabilities = 0x10000000
	V4L2CapIoMc               V4L2CapabilityCapabilities = 0x20000000
	V4L2CapDeviceCaps         V4L2CapabilityCapabilities = 0x80000000
)

// https://docs.kernel.org/userspace-api/media/v4l/vidioc-querycap.html#c.V4L.V4L2Capability
type V4L2Capability struct {
	_ structs.HostLayout

	Driver       [16]uint8
	Card         [32]uint8
	BusInfo      [32]uint8
	Version      uint32
	Capabilities uint32
	DeviceCaps   uint32
	Reserved     [3]uint32
}

func V4L2DiscoverDevices() (map[string]V4L2Capability, error) {
	results := map[string]V4L2Capability{}
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

	for _, candidate := range candidates {
		canonical := candidate
		if resolved, err := filepath.EvalSymlinks(candidate); err == nil {
			canonical = resolved
		}

		if _, exists := seenCanonical[canonical]; exists {
			continue
		}

		capability, err := V4L2DeviceCapability(filepath.Base(canonical))
		if err != nil {
			continue
		}

		if !capability.HasCapabilities(V4L2CapVideoCapture) {
			continue
		}

		results[filepath.Base(canonical)] = capability
		seenCanonical[canonical] = struct{}{}
	}

	return results, nil

}

// QueryDevice returns V4L2 capability information for the given device path.
func V4L2DeviceCapability(filename string) (V4L2Capability, error) {
	var cap V4L2Capability

	f, err := os.OpenFile(filepath.Join("/dev", filename), os.O_RDONLY|int(unix.O_NONBLOCK), 0)
	if err != nil {
		return cap, fmt.Errorf("open device %s: %w", filename, err)
	}
	defer f.Close()

	_, _, errno := unix.Syscall(unix.SYS_IOCTL, f.Fd(), uintptr(VIDIOC_QUERYCAP), uintptr(unsafe.Pointer(&cap)))
	if errno != 0 {
		return cap, fmt.Errorf("VIDIOC_QUERYCAP ioctl failed: %v", errno)
	}

	return cap, nil
}

func (c V4L2Capability) DriverString() string {
	return cString(c.Driver[:])
}

func (c V4L2Capability) CardString() string {
	return cString(c.Card[:])
}

func (c V4L2Capability) BusInfoString() string {
	return cString(c.BusInfo[:])
}

func (c V4L2Capability) VersionString() string {
	major := (c.Version >> 16) & 0xFF
	minor := (c.Version >> 8) & 0xFF
	patch := c.Version & 0xFF
	return fmt.Sprintf("%d.%d.%d", major, minor, patch)
}

func (c V4L2Capability) HasCapabilities(caps ...V4L2CapabilityCapabilities) bool {
	if len(caps) == 0 {
		return false
	}

	effectiveCaps := c.Capabilities
	if (c.Capabilities & uint32(V4L2CapDeviceCaps)) != 0 {
		effectiveCaps = c.DeviceCaps
	}

	for _, cap := range caps {
		if (effectiveCaps & uint32(cap)) == 0 {
			return false
		}
	}
	return true
}

func cString(b []byte) string {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		return string(b[:i])
	}
	return string(bytes.TrimRight(b, "\x00"))
}
