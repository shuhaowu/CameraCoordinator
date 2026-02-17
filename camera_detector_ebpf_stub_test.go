//go:build !ebpf

package cameracoordinator

import (
	"context"
	"errors"
	"testing"
)

func TestEBPFDetectorStubReturnsUnavailable(t *testing.T) {
	t.Parallel()

	detector := NewEBPFCameraDetector()
	if detector.Events() == nil {
		t.Fatal("detector events channel is nil")
	}

	err := detector.Run(context.Background())
	if !errors.Is(err, ErrEBPFUnavailable) {
		t.Fatalf("Run() error = %v, want %v", err, ErrEBPFUnavailable)
	}
}
