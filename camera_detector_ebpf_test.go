package cameracoordinator

import (
	"testing"
)

func TestEBPFDetectorEventsChannelInitialized(t *testing.T) {
	t.Parallel()

	detector := NewEBPFVb2IoctlStreamDetector()
	if detector.Events() == nil {
		t.Fatal("detector events channel is nil")
	}
}
