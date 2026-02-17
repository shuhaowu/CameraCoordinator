package cameracoordinator

import "testing"

func TestEBPFDetectorEventsChannelInitialized(t *testing.T) {
	t.Parallel()

	detector := NewEBPFCameraDetector()
	if detector.Events() == nil {
		t.Fatal("detector events channel is nil")
	}
}
