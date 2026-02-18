package cameracoordinator

type CameraEventType uint32

const (
	// Synchronize this with camera_event_type in bpf/camera_detector_vb2_ioctl.bpf.c
	CameraEventRecordingOn  CameraEventType = 1
	CameraEventRecordingOff CameraEventType = 2
)

type CameraEvent struct {
	// The string identifying the detector that emitted this event. This is useful for debugging.
	Detector string

	// The type of the event.
	Type CameraEventType

	// The video device file associated with this event, e.g. "video0".
	VideoDevice string
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
