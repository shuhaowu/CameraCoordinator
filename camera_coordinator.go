package cameracoordinator

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
)

// CameraCoordinator aggregates one or more detector event streams into a single
// logical view of camera state. It deduplicates events across detectors (emits
// one RecordingOn when the first detector goes active for a device, one
// RecordingOff when the last one goes inactive) and fans out the resulting
// events to a set of output channels provided at construction time.
type CameraCoordinator struct {
	outputs []chan CameraEvent
	started atomic.Bool

	// queryV4L2Cap looks up the V4L2 capability for the given device filename
	// (e.g. "video0"). It is injectable so tests can stub the syscall.
	queryV4L2Cap func(string) (V4L2Capability, error)
}

// NewCameraCoordinator creates a CameraCoordinator that fans out deduplicated
// camera events to the supplied output channels.
func NewCameraCoordinator(outputs []chan CameraEvent) *CameraCoordinator {
	return &CameraCoordinator{
		outputs:      outputs,
		queryV4L2Cap: V4L2DeviceCapability,
	}
}

// Run monitors the supplied events channel for camera on/off events and
// statefully emits coordinator-level on/off events to the output channels
// provided at construction time.
//
// The caller is responsible for starting the detectors and providing their
// combined events channel. Run returns when ctx is cancelled or events is
// closed (i.e. all detectors have finished).
//
// A camera on event is generated on the first event from the detectors for that
// device. A camera off event is generated on the last event from the detectors
// for that device.
//
// The events in the buffer are not guaranteed to be processed on context
// cancellation.
//
// Run can only be called once.
func (c *CameraCoordinator) Run(ctx context.Context, events <-chan CameraEvent) error {
	if c.started.Swap(true) {
		return errors.New("camera coordinator can only be started once")
	}

	logger := slog.With("component", "CameraCoordinator")

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
	//
	// TODO: that said maybe the multi-detector idea is a bit overkill anyways. So
	// a lot of this could be removed.
	active := make(map[string]map[string]bool)

	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-events:
			if !ok {
				// events channel closed: all detectors have finished.
				logger.Debug("events channel closed, camera coordinator done")
				return nil
			}

			logger.Debug("received event",
				"detector", ev.Detector,
				"type", ev.Type.String(),
				"video_device", ev.VideoDevice,
			)

			// Always query the V4L2 capability fresh so we pick up changes
			// after a device is unplugged and replugged.
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
					c.broadcastEvent(ctx, CameraEvent{
						Detector:    "coordinator",
						Type:        CameraEventRecordingOn,
						VideoDevice: ev.VideoDevice,
						Card:        cap.CardString(),
						BusInfo:     cap.BusInfoString(),
					})
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
					c.broadcastEvent(ctx, CameraEvent{
						Detector:    "coordinator",
						Type:        CameraEventRecordingOff,
						VideoDevice: ev.VideoDevice,
						Card:        cap.CardString(),
						BusInfo:     cap.BusInfoString(),
					})
				}

			default:
				logger.Warn("unknown event type", "type", ev.Type, "video_device", ev.VideoDevice, "detector", ev.Detector)
			}
		}
	}
}

// broadcastEvent sends ev to every output channel, respecting ctx cancellation.
func (c *CameraCoordinator) broadcastEvent(ctx context.Context, ev CameraEvent) {
	for _, out := range c.outputs {
		select {
		case <-ctx.Done():
			return
		case out <- ev:
		}
	}
}
