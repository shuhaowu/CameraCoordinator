package cameracoordinator

import (
	"bytes"
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/unix"
)

// VideoDeviceInfo holds human readable metadata returned by VIDIOC_QUERYCAP
type VideoDeviceInfo struct {
	Driver       string
	Card         string
	BusInfo      string
	Version      uint32
	Capabilities uint32
	DeviceCaps   uint32
}

// QueryV4L2Device opens the given video device (e.g. /dev/video0) and
// returns its V4L2 capability information using the VIDIOC_QUERYCAP ioctl.
func QueryV4L2Device(dev string) (VideoDeviceInfo, error) {
	var out VideoDeviceInfo

	f, err := os.OpenFile(dev, os.O_RDONLY|int(unix.O_NONBLOCK), 0)
	if err != nil {
		return out, fmt.Errorf("open device %s: %w", dev, err)
	}
	defer f.Close()

	var cap v4l2_capability

	// Perform ioctl: VIDIOC_QUERYCAP
	const VIDIOC_QUERYCAP = 0x80685600

	_, _, errno := unix.Syscall(unix.SYS_IOCTL, f.Fd(), uintptr(VIDIOC_QUERYCAP), uintptr(unsafe.Pointer(&cap)))
	if errno != 0 {
		return out, fmt.Errorf("VIDIOC_QUERYCAP ioctl failed: %v", errno)
	}

	out.Driver = cString(cap.Driver[:])
	out.Card = cString(cap.Card[:])
	out.BusInfo = cString(cap.BusInfo[:])
	out.Version = cap.Version
	out.Capabilities = cap.Capabilities
	out.DeviceCaps = cap.DeviceCaps

	return out, nil
}

// v4l2_capability mirrors the C struct used by VIDIOC_QUERYCAP.
type v4l2_capability struct {
	Driver       [16]byte
	Card         [32]byte
	BusInfo      [32]byte
	Version      uint32
	Capabilities uint32
	DeviceCaps   uint32
	Reserved     [3]uint32
}

func cString(b []byte) string {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		return string(b[:i])
	}
	return string(bytes.TrimRight(b, "\x00"))
}
