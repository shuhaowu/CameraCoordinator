package cameracoordinator

import (
	"testing"
	"unsafe"
)

// This test ensures the manually-defined VIDIOC_QUERYCAP constant encodes the
// size of the Go `V4L2Capability` struct. If the struct layout changes the
// ioctl encoding will no longer match and this test will fail — preventing a
// silent runtime mismatch.
//
// Warning: AI-generated without verification
func TestVIDIOCQuerycapEncodesStructSize(t *testing.T) {
	// Replicate the Linux _IOC/_IOR encoding used for VIDIOC_QUERYCAP.
	const (
		iocNRBits   = 8
		iocTypeBits = 8
		iocSizeBits = 14
		iocDirBits  = 2
	)
	const (
		iocNRShift   = 0
		iocTypeShift = iocNRShift + iocNRBits
		iocSizeShift = iocTypeShift + iocTypeBits
		iocDirShift  = iocSizeShift + iocSizeBits
	)
	const iocRead = 2 // _IOC_READ

	expected := uint32((uint32(iocRead) << iocDirShift) | (uint32('V') << iocTypeShift) | (uint32(0) << iocNRShift) | (uint32(unsafe.Sizeof(V4L2Capability{})) << iocSizeShift))
	if uint32(VIDIOC_QUERYCAP) != expected {
		t.Fatalf("VIDIOC_QUERYCAP (0x%x) does not match expected _IOR encoding (0x%x); update constant or struct", VIDIOC_QUERYCAP, expected)
	}
}
