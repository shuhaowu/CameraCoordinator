package cameracoordinator

import (
	"context"
	"errors"
	"log/slog"
	"sync"
)

type CameraCoordinator struct {
	detectors []CameraDetector
	events    chan CameraEvent
}

func NewCameraCoordinator(detectors ...CameraDetector) *CameraCoordinator {
	return &CameraCoordinator{
		detectors: detectors,
		events:    make(chan CameraEvent),
	}
}

func (c *CameraCoordinator) Events() <-chan CameraEvent {
	return c.events
}

// Run starts all child detectors and fans their events into the coordinator
// event channel. It waits for all child detectors to exit, logs any non-
// canceled errors via the slog package, and always returns nil. The
// coordinator closes its events channel before returning.
func (c *CameraCoordinator) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var feedersWg sync.WaitGroup
	var runnersWg sync.WaitGroup

	// Start feeders and detector runners.
	for _, d := range c.detectors {
		// Capture local loop variable for goroutines.
		det := d

		// feeder: copy events from child detector to coordinator channel
		feedersWg.Go(func() {
			for {
				select {
				case <-ctx.Done():
					return
				case ev, ok := <-det.Events():
					if !ok {
						return
					}
					select {
					case <-ctx.Done():
						return
					case c.events <- ev:
					}
				}
			}
		})

		// runner: execute detector lifecycle and log errors
		runnersWg.Go(func() {
			if err := det.Run(ctx); err != nil {
				if !errors.Is(err, context.Canceled) {
					slog.Error("detector run error", "detector", det.Name(), "err", err)
				}
			}
		})
	}

	// Wait for all runners to finish. We do not return their errors; we log
	// them above and always return nil as requested.
	runnersWg.Wait()

	// Signal feeders to stop (in case detectors did not close their event channels).
	cancel()

	// Wait for feeders to exit and close events channel.
	feedersWg.Wait()
	close(c.events)

	return nil
}
