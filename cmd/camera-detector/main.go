package main

import (
	"context"
	"log/slog"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/shuhaowu/cameracoordinator"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	detector := cameracoordinator.NewEBPFVb2IoctlStreamDetector()
	coord := cameracoordinator.NewCameraCoordinator(detector)

	var wg sync.WaitGroup

	// Run the coordinator in background; it logs errors itself and returns nil.
	wg.Add(1)
	go func() {
		defer wg.Done()
		slog.Info("detecting when camera recording starts/stops...")
		_ = coord.Run(ctx)
	}()

	defer wg.Wait()

	for {
		select {
		case <-ctx.Done():
			return
		case event := <-coord.Events():
			slog.Info("camera event",
				"time", time.Now().Format(time.RFC3339Nano),
				"event", event.Type.String(),
				"device", event.VideoFilename,
			)
		}
	}
}
