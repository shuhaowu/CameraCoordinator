//go:build integration

package cameracoordinator

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestEBPFCameraDetectorAttachIntegration(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("integration test requires root privileges")
	}

	detector := NewEBPFCameraDetector()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err := detector.Run(ctx)
	if err == nil {
		return
	}

	// Kernel symbol availability differs by distro/kernel config. Skip when unsupported.
	if strings.Contains(err.Error(), "attach kprobe") || strings.Contains(err.Error(), "load bpf objects") {
		t.Skipf("kernel/environment does not support this integration path: %v", err)
	}

	t.Fatalf("unexpected detector run error: %v", err)
}
