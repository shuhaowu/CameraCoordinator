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

// Run starts the child detectors and monitors for camera on/off events
// statefully. Internally, it has three groups of goroutines:
//
//  1. A group of detector goroutines that runs the detector logic.
//  2. A group of forwarder goroutines that forwards the events channel from each
//     detector into a single channel
//  3. A single event handler goroutine that receives all events from all
//     detectors and statefully tracks the camera on/off state.
//
// A camera on event is generated on the first event from the detectors for that
// device. A camera off event is generated on the last event from the detectors
// for that device.
func (c *CameraCoordinator) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	logger := slog.With("component", "CameraCoordinator")

	allEvents := make(chan CameraEvent)

	var detectorWg sync.WaitGroup
	var forwarderWg sync.WaitGroup
	var eventHandlerWg sync.WaitGroup
	for _, d := range c.detectors {
		det := d

		detectorWg.Go(func() {
			innerLogger := logger.With("detector", det.Name())
			innerLogger.Debug("starting detector")
			defer innerLogger.Debug("stopping detector")

			err := det.Run(ctx)
			if err != nil && !errors.Is(err, context.Canceled) {
				innerLogger.Error("detector ran into an error", "err", err)
				// TODO: send the error back!
			}
		})

		forwarderWg.Go(func() {
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
					case allEvents <- ev:
					}
				}
			}
		})
	}

	eventHandlerWg.Go(func() {
		// Track active "on" counts per video device. The coordinator emits a
		// CameraEventRecordingOn when the count transitions 0->1 and emits a
		// CameraEventRecordingOff when the count transitions 1->0.
		active := make(map[string]int)

		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-allEvents:
				if !ok {
					return
				}

				logger.Debug("received event",
					"detector", ev.Detector,
					"type", ev.Type.String(),
					"video_device", ev.VideoDevice,
				)

				switch ev.Type {
				case CameraEventRecordingOn:
					prev := active[ev.VideoDevice]
					active[ev.VideoDevice] = prev + 1
					if prev == 0 {
						// first active detector for this device -> forward ON
						logger.Debug("emitting camera on event", "video_device", ev.VideoDevice)
						c.emitEvent(ctx, CameraEvent{Detector: ev.Detector, Type: CameraEventRecordingOn, VideoDevice: ev.VideoDevice})
					}

				case CameraEventRecordingOff:
					prev, ok := active[ev.VideoDevice]
					if !ok || prev == 0 {
						// stray/off without prior on -> ignore
						continue
					}

					if prev == 1 {
						// last active detector -> forward OFF and remove state
						delete(active, ev.VideoDevice)
						logger.Debug("emitting camera off event", "video_device", ev.VideoDevice)
						c.emitEvent(ctx, CameraEvent{Detector: "coordinator", Type: CameraEventRecordingOff, VideoDevice: ev.VideoDevice})
					} else {
						active[ev.VideoDevice] = prev - 1
					}

				default:
					logger.Warn("unknown event type", "type", ev.Type, "video_device", ev.VideoDevice, "detector", ev.Detector)
				}
			}
		}
	})

	detectorWg.Wait()

	logger.Debug("all detector has quit, waiting for forwarders to finish...")

	// If all detectors have quit or failed, cancel the remaining goroutines.
	cancel()

	forwarderWg.Wait()

	logger.Debug("all forwarder has quit, waiting for event handler to finish...")

	eventHandlerWg.Wait()

	logger.Debug("camera coordinator done")

	close(c.events)

	return nil
}

// emitEvent sends an event to the public events channel but respects the
// provided context (returns immediately if ctx is done).
func (c *CameraCoordinator) emitEvent(ctx context.Context, ev CameraEvent) {
	select {
	case <-ctx.Done():
		return
	case c.events <- ev:
	}
}
