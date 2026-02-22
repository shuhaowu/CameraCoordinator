package cameracoordinator

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"
)

func init() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})))
}

// newTestCoordinator creates a CameraCoordinator wired with a stub
// queryV4L2Cap so tests never hit real Linux syscalls. The stub reports every
// device as a valid video-capture device.
func newTestCoordinator() *CameraCoordinator {
	c := NewCameraCoordinator()
	c.queryV4L2Cap = func(string) (V4L2Capability, error) {
		return V4L2Capability{Capabilities: uint32(V4L2CapVideoCapture)}, nil
	}
	return c
}

const defaultTimeout = 50 * time.Millisecond

func TestCameraCoordinatorEmitsOnlyFirstOnPerDeviceAcrossDetectors(t *testing.T) {
	// This verifies cross-detector shared state for the same video device: multiple
	// on signals from different detectors collapse to a single emitted on event.
	coordinator := newTestCoordinator()
	allEvents := make(chan CameraEvent, 16)

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Go(func() {
		coordinator.Run(ctx, allEvents)
	})
	defer func() {
		cancel()
		wg.Wait()
	}()

	allEvents <- CameraEvent{Detector: "A", Type: CameraEventRecordingOn, VideoDevice: "video0"}

	evs := receiveEvents(t, coordinator.Events(), 1, defaultTimeout)

	if len(evs) != 1 {
		t.Fatalf("expected exactly one event, got %d (events: %v)", len(evs), evs)
	}

	ev := evs[0]
	assertEvent(t, ev, CameraEventRecordingOn, "video0")

	allEvents <- CameraEvent{Detector: "B", Type: CameraEventRecordingOn, VideoDevice: "video0"}

	// no new events should be emitted since the coordinator should ignore the second ON for the same device
	evs = receiveEvents(t, coordinator.Events(), 1, defaultTimeout)
	if len(evs) != 0 {
		t.Fatalf("expected no events, got %d (events: %v)", len(evs), evs)
	}
}

func TestCameraCoordinatorIgnoresOffWithoutPriorOn(t *testing.T) {
	// Simpler concurrency: stray OFF must be ignored and only ON->OFF be emitted.
	coordinator := newTestCoordinator()
	allEvents := make(chan CameraEvent, 16)

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Go(func() {
		coordinator.Run(ctx, allEvents)
	})
	defer func() {
		cancel()
		wg.Wait()
	}()

	allEvents <- CameraEvent{Type: CameraEventRecordingOff, VideoDevice: "video9"}

	// no events should be emitted since the OFF is stray without a prior ON
	evs := receiveEvents(t, coordinator.Events(), 1, defaultTimeout)
	if len(evs) != 0 {
		t.Fatalf("video9: expected no events, got %d (events: %v)", len(evs), evs)
	}

	allEvents <- CameraEvent{Type: CameraEventRecordingOn, VideoDevice: "video9"}
	allEvents <- CameraEvent{Type: CameraEventRecordingOff, VideoDevice: "video9"}

	// collect exactly two forwarded events (ON then OFF)
	got := receiveEvents(t, coordinator.Events(), 2, defaultTimeout)

	want := []CameraEvent{{Type: CameraEventRecordingOn, VideoDevice: "video9"}, {Type: CameraEventRecordingOff, VideoDevice: "video9"}}
	if len(got) != len(want) {
		t.Fatalf("video9: expected %d events, got %d (events: %v)", len(want), len(got), got)
	}
	for i := range want {
		assertEvent(t, got[i], want[i].Type, want[i].VideoDevice)
	}
}

func TestCameraCoordinatorDuplicateOnsFromSameDetectorCountOnce(t *testing.T) {
	// A single detector that fires multiple consecutive ON events for the same
	// device must only be counted once.  The first OFF from that detector should
	// therefore immediately emit a coordinator OFF — not require a matching number
	// of OFFs equal to the number of ONs received.
	coordinator := newTestCoordinator()
	allEvents := make(chan CameraEvent, 16)

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Go(func() {
		coordinator.Run(ctx, allEvents)
	})
	defer func() {
		cancel()
		wg.Wait()
	}()

	// First ON — should produce exactly one coordinator ON.
	allEvents <- CameraEvent{Detector: "det", Type: CameraEventRecordingOn, VideoDevice: "video2"}
	// Second ON from the *same* detector — must be deduplicated.
	allEvents <- CameraEvent{Detector: "det", Type: CameraEventRecordingOn, VideoDevice: "video2"}

	evs := receiveEvents(t, coordinator.Events(), 1, defaultTimeout)
	if len(evs) != 1 {
		t.Fatalf("video2: expected exactly one ON event, got %d (events: %v)", len(evs), evs)
	}
	assertEvent(t, evs[0], CameraEventRecordingOn, "video2")

	// First OFF — because only one unique detector is active, this should
	// immediately produce a coordinator OFF (not be absorbed by a phantom count).
	allEvents <- CameraEvent{Detector: "det", Type: CameraEventRecordingOff, VideoDevice: "video2"}

	evs = receiveEvents(t, coordinator.Events(), 1, defaultTimeout)
	if len(evs) != 1 {
		t.Fatalf("video2: expected exactly one OFF event after first OFF, got %d (events: %v)", len(evs), evs)
	}
	assertEvent(t, evs[0], CameraEventRecordingOff, "video2")

	// Any further OFFs are stray and must be ignored.
	allEvents <- CameraEvent{Detector: "det", Type: CameraEventRecordingOff, VideoDevice: "video2"}
	evs = receiveEvents(t, coordinator.Events(), 1, defaultTimeout)
	if len(evs) != 0 {
		t.Fatalf("video2: expected no events for stray OFF, got %d (events: %v)", len(evs), evs)
	}
}

func TestCameraCoordinatorMultipleDetectorsRequireAllOffsBeforeEmittingOff(t *testing.T) {
	// When two *different* detectors both report ON for the same device, the
	// coordinator must wait until both have sent an OFF before forwarding the
	// coordinator-level OFF event.  This verifies that the per-detector dedup
	// only collapses repeated events from the *same* detector, not distinct ones.
	coordinator := newTestCoordinator()
	allEvents := make(chan CameraEvent, 16)

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Go(func() {
		coordinator.Run(ctx, allEvents)
	})
	defer func() {
		cancel()
		wg.Wait()
	}()

	// Both detectors fire ON for the same device.
	allEvents <- CameraEvent{Detector: "A", Type: CameraEventRecordingOn, VideoDevice: "video3"}
	allEvents <- CameraEvent{Detector: "B", Type: CameraEventRecordingOn, VideoDevice: "video3"}

	// Only one coordinator ON should be emitted.
	evs := receiveEvents(t, coordinator.Events(), 1, defaultTimeout)
	if len(evs) != 1 {
		t.Fatalf("video3: expected exactly one ON event, got %d (events: %v)", len(evs), evs)
	}
	assertEvent(t, evs[0], CameraEventRecordingOn, "video3")

	// Detector A sends OFF — B is still active, so no coordinator OFF yet.
	allEvents <- CameraEvent{Detector: "A", Type: CameraEventRecordingOff, VideoDevice: "video3"}
	evs = receiveEvents(t, coordinator.Events(), 1, defaultTimeout)
	if len(evs) != 0 {
		t.Fatalf("video3: expected no OFF event while detector B is still active, got %d (events: %v)", len(evs), evs)
	}

	// Detector B sends OFF — now all detectors are off, coordinator OFF is emitted.
	allEvents <- CameraEvent{Detector: "B", Type: CameraEventRecordingOff, VideoDevice: "video3"}
	evs = receiveEvents(t, coordinator.Events(), 1, defaultTimeout)
	if len(evs) != 1 {
		t.Fatalf("video3: expected exactly one OFF event after both detectors are off, got %d (events: %v)", len(evs), evs)
	}
	assertEvent(t, evs[0], CameraEventRecordingOff, "video3")
}

func TestCameraCoordinatorIgnoresRepeatedOffsWithoutPriorOn(t *testing.T) {
	// Multiple stray OFFs must all be ignored and must not cause the
	// internal counter to go negative.
	coordinator := newTestCoordinator()
	allEvents := make(chan CameraEvent, 16)

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Go(func() {
		coordinator.Run(ctx, allEvents)
	})
	defer func() {
		cancel()
		wg.Wait()
	}()

	// send several OFFs in quick succession
	allEvents <- CameraEvent{Type: CameraEventRecordingOff, VideoDevice: "videoX"}
	allEvents <- CameraEvent{Type: CameraEventRecordingOff, VideoDevice: "videoX"}
	allEvents <- CameraEvent{Type: CameraEventRecordingOff, VideoDevice: "videoX"}

	evs := receiveEvents(t, coordinator.Events(), 1, defaultTimeout)
	if len(evs) != 0 {
		t.Fatalf("expected no events for repeated stray OFFs, got %d", len(evs))
	}
}

func TestCameraCoordinatorDoesNotUndercount(t *testing.T) {
	// A single ON followed by multiple OFFs should emit exactly one ON and
	// one OFF; subsequent OFFs must be ignored (no negative counts).
	coordinator := newTestCoordinator()
	allEvents := make(chan CameraEvent, 16)

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Go(func() {
		coordinator.Run(ctx, allEvents)
	})
	defer func() {
		cancel()
		wg.Wait()
	}()

	// emit ON
	allEvents <- CameraEvent{Type: CameraEventRecordingOn, VideoDevice: "videoY"}
	evs := receiveEvents(t, coordinator.Events(), 1, defaultTimeout)
	assertEvent(t, evs[0], CameraEventRecordingOn, "videoY")

	// emit OFF (first) -> should forward OFF
	allEvents <- CameraEvent{Type: CameraEventRecordingOff, VideoDevice: "videoY"}
	evs = receiveEvents(t, coordinator.Events(), 1, defaultTimeout)
	assertEvent(t, evs[0], CameraEventRecordingOff, "videoY")

	// additional OFFs should be ignored
	allEvents <- CameraEvent{Type: CameraEventRecordingOff, VideoDevice: "videoY"}
	allEvents <- CameraEvent{Type: CameraEventRecordingOff, VideoDevice: "videoY"}
	evs = receiveEvents(t, coordinator.Events(), 1, defaultTimeout)
	if len(evs) != 0 {
		t.Fatalf("expected no events after redundant OFFs, got %d", len(evs))
	}
}

func TestCameraCoordinatorIgnoresNonCaptureDevice(t *testing.T) {
	// Devices that are not video-capture must be ignored by the coordinator.
	coordinator := NewCameraCoordinator()

	// Override probe to report the device as NOT having the video-capture capability.
	coordinator.queryV4L2Cap = func(string) (V4L2Capability, error) {
		return V4L2Capability{Capabilities: 0}, nil
	}

	allEvents := make(chan CameraEvent, 16)

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Go(func() {
		coordinator.Run(ctx, allEvents)
	})
	defer func() {
		cancel()
		wg.Wait()
	}()

	allEvents <- CameraEvent{Type: CameraEventRecordingOn, VideoDevice: "videoNotCapture"}

	// No events should be emitted because the device isn't a capture device.
	evs := receiveEvents(t, coordinator.Events(), 1, defaultTimeout)
	if len(evs) != 0 {
		t.Fatalf("expected no events for non-capture device, got %d (events: %v)", len(evs), evs)
	}

	// And OFFs should also be ignored.
	allEvents <- CameraEvent{Type: CameraEventRecordingOff, VideoDevice: "videoNotCapture"}
	evs = receiveEvents(t, coordinator.Events(), 1, defaultTimeout)
	if len(evs) != 0 {
		t.Fatalf("expected no events for non-capture device OFF, got %d (events: %v)", len(evs), evs)
	}
}

func receiveEvents(t *testing.T, ch <-chan CameraEvent, count int, timeout time.Duration) []CameraEvent {
	t.Helper()
	evs := make([]CameraEvent, 0, count)
	if count == 0 {
		panic("receiveEvents: count must be > 0")
	}

	for i := 0; i < count; i++ {
		select {
		case ev, ok := <-ch:
			if !ok {
				return evs
			}
			evs = append(evs, ev)
		case <-time.After(timeout):
			return evs
		}
	}
	return evs
}

func assertEvent(t *testing.T, ev CameraEvent, wantType CameraEventType, wantDevice string) {
	t.Helper()
	if ev.Type != wantType {
		t.Fatalf("expected event type to be %s, got %s", wantType.String(), ev.Type.String())
	}
	if ev.VideoDevice != wantDevice {
		t.Fatalf("expected video device to be %s, got %s", wantDevice, ev.VideoDevice)
	}
}
