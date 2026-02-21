package cameracoordinator

import (
	"context"
	"log/slog"
)

// Notifier processes camera events from a CameraCoordinator. Implementations
// are intended to be started in a background goroutine via Run.
type Notifier interface {
	// Run reads from events and processes them until ctx is cancelled or the
	// channel is closed. It is designed to be called in a goroutine.
	Run(ctx context.Context, events <-chan CameraEvent) error
}

// PrintNotifier is a Notifier that logs camera recording on/off events using
// slog.
type PrintNotifier struct{}

func NewPrintNotifier() *PrintNotifier {
	return &PrintNotifier{}
}

// Run logs each CameraEvent until ctx is cancelled or events is closed.
func (p *PrintNotifier) Run(ctx context.Context, events <-chan CameraEvent) error {
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
