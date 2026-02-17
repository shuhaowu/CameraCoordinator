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
