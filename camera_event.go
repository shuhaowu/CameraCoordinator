package cameracoordinator

type CameraEventType uint8

const (
	// Synchronize this with camera_event_type in bpf/camera_detector_vb2_ioctl.bpf.c
	CameraEventRecordingOn  CameraEventType = 1
	CameraEventRecordingOff CameraEventType = 2
)

type CameraEvent struct {
	Type CameraEventType

	VideoFilename string
}

func (t CameraEventType) String() string {
	switch t {
	case CameraEventRecordingOn:
		return "recording_on"
	case CameraEventRecordingOff:
		return "recording_off"
	default:
		return "unknown"
	}
}
