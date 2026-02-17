package cameracoordinator

import (
	"strings"
)

type ebpfRawEvent struct {
	EventType uint8
	_         [3]byte
	Name      [16]byte
}

func mapRawEventToCameraEvent(event ebpfRawEvent) (CameraEvent, bool) {
	name := normalizeVideoFilename(cStringToGo(event.Name[:]))
	if name == "" {
		return CameraEvent{}, false
	}

	switch event.EventType {
	case 1:
		return CameraEvent{Type: CameraEventRecordingOn, VideoFilename: name}, true
	case 2:
		return CameraEvent{Type: CameraEventRecordingOff, VideoFilename: name}, true
	default:
		return CameraEvent{}, false
	}
}

func normalizeVideoFilename(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return ""
	}

	if strings.HasPrefix(trimmed, "/dev/") {
		return trimmed
	}

	return "/dev/" + trimmed
}

func cStringToGo(raw []byte) string {
	for index := range raw {
		if raw[index] == 0 {
			return string(raw[:index])
		}
	}

	return string(raw)
}
