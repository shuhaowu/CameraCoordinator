package cameracoordinator

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
)

type CameraCoordinator struct {
	detectors []CameraDetector
	events    chan CameraEvent
	started   atomic.Bool

	// queryV4L2Cap looks up the V4L2 capability for the given device filename
	// (e.g. "video0"). It is injectable so tests can stub the syscall.
	queryV4L2Cap func(string) (V4L2Capability, error)
}

func NewCameraCoordinator(detectors ...CameraDetector) *CameraCoordinator {
	return &CameraCoordinator{
		detectors: detectors,
		events:    make(chan CameraEvent),

		queryV4L2Cap: V4L2DeviceCapability,
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
//
// The events in the buffer are not guaranteed to be processed on context
// cancellation.
//
// Run can only be called once.
func (c *CameraCoordinator) Run(ctx context.Context) error {
	if c.started.Swap(true) {
		return errors.New("camera coordinator can only be started once")
	}

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
		// Track which detectors are currently "on" per video device.
		// Key: videoDevice → set of detector names that have sent an ON without a
		// matching OFF.  The coordinator emits CameraEventRecordingOn when the set
		// transitions from empty to non-empty (first unique detector goes active)
		// and emits CameraEventRecordingOff when the set transitions back to empty
		// (last active detector sends OFF).
		//
		// Consecutive ON events from the *same* detector for the same device are
		// deduplicated: only the first counts.  This means a rogue detector that
		// fires ON multiple times cannot inflate the reference count and prevent the
		// coordinator from ever emitting the matching OFF.
		active := make(map[string]map[string]bool)

		for {
			select {
			case <-ctx.Done():
				return
			case ev := <-allEvents:
				// TODO: events maybe dropped on shutdown. This is OK for now.
				// In the future we can refactor so that the emit logic below is a
				// instance function and we can drain after the everything shuts down.

				logger.Debug("received event",
					"detector", ev.Detector,
					"type", ev.Type.String(),
					"video_device", ev.VideoDevice,
				)

				// Always query the V4L2 capability fresh so we pick up changes
				// after a device is unplugged and replugged.
				// Only enforce the VideoCapture gate when the device is not
				// already being tracked: if it is active, we must still process
				// its OFF even when the device node is gone.
				cap, err := c.queryV4L2Cap(ev.VideoDevice)
				if err != nil {
					logger.Warn("failed to query V4L2 capabilities, skipping event",
						"err", err, "video_device", ev.VideoDevice)
					continue
				} else if !cap.HasCapabilities(V4L2CapVideoCapture) {
					logger.Debug("device is not a video capture device, skipping event",
						"video_device", ev.VideoDevice)
					continue
				}

				switch ev.Type {
				case CameraEventRecordingOn:
					if active[ev.VideoDevice] == nil {
						active[ev.VideoDevice] = make(map[string]bool)
					}
					if active[ev.VideoDevice][ev.Detector] {
						// This detector already has an outstanding ON for this
						// device — ignore the duplicate so it does not inflate
						// the reference count.
						logger.Debug("duplicate on from same detector; ignoring",
							"detector", ev.Detector,
							"video_device", ev.VideoDevice,
						)
						break
					}
					wasEmpty := len(active[ev.VideoDevice]) == 0
					active[ev.VideoDevice][ev.Detector] = true
					if wasEmpty {
						// first active detector for this device -> forward ON
						logger.Debug("emitting camera on event", "video_device", ev.VideoDevice)
						c.emitEvent(ctx, CameraEvent{Detector: "coordinator", Type: CameraEventRecordingOn, VideoDevice: ev.VideoDevice, Capability: cap})
					}

				case CameraEventRecordingOff:
					detectors, exists := active[ev.VideoDevice]
					if !exists || !detectors[ev.Detector] {
						// No prior ON from this detector — treat as stray and ignore.
						logger.Debug("stray/off without prior on; ignoring",
							"detector", ev.Detector,
							"video_device", ev.VideoDevice,
						)
						break
					}
					delete(detectors, ev.Detector)
					if len(detectors) == 0 {
						// last active detector turned off -> forward OFF and remove state
						delete(active, ev.VideoDevice)
						logger.Debug("emitting camera off event", "video_device", ev.VideoDevice)
						c.emitEvent(ctx, CameraEvent{Detector: "coordinator", Type: CameraEventRecordingOff, VideoDevice: ev.VideoDevice, Capability: cap})
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
