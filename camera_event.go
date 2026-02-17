package cameracoordinator

type CameraEventType uint8

const (
	CameraEventRecordingOn CameraEventType = iota
	CameraEventRecordingOff
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
