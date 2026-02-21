package cameracoordinator

import (
	"context"
	"log/slog"
)

// Adapter processes camera events from a CameraCoordinator. Implementations
// are intended to be started in a background goroutine via Run.
type Adapter interface {
	// Run reads from events and processes them until ctx is cancelled or the
	// channel is closed. It is designed to be called in a goroutine.
	Run(ctx context.Context, events <-chan CameraEvent) error
}

// PrintAdapter is an Adapter that logs camera recording on/off events using
// slog.
type PrintAdapter struct{}

func NewPrintAdapter() *PrintAdapter {
	return &PrintAdapter{}
}

// Run logs each CameraEvent until ctx is cancelled or events is closed.
func (p *PrintAdapter) Run(ctx context.Context, events <-chan CameraEvent) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case event, open := <-events:
			if !open {
				return nil
			}

			slog.Info("camera event",
				"event", event.Type.String(),
				"device", event.VideoDevice,
			)
		}
	}
}
