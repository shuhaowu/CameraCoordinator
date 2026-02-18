package cameracoordinator

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"
)

type testDetector struct {
	events chan CameraEvent
}

func newControllableDetector() *testDetector {
	return &testDetector{
		events: make(chan CameraEvent, 16),
	}
}

func (d *testDetector) Events() <-chan CameraEvent {
	return d.events
}

func (d *testDetector) Name() string {
	return "testDetector"
}

func (d *testDetector) Run(ctx context.Context) error {
	// Tests control `d.events` directly (send + close). This Run simply
	// waits for the provided context to be cancelled so the test can decide
	// when the detector has finished.
	<-ctx.Done()
	return ctx.Err()
}

func init() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})))
}

const defaultTimeout = 50 * time.Millisecond

func TestCameraCoordinatorEmitsOnlyFirstOnPerDeviceAcrossDetectors(t *testing.T) {
	// This verifies cross-detector shared state for the same video device: multiple
	// on signals from different detectors collapse to a single emitted on event.
	detectorA := newControllableDetector()
	detectorB := newControllableDetector()
	coordinator := NewCameraCoordinator(detectorA, detectorB)

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Go(func() {
		coordinator.Run(ctx)
	})
	defer func() {
		close(detectorA.events)
		close(detectorB.events)
		cancel()
		wg.Wait()
	}()

	detectorA.events <- CameraEvent{Detector: "A", Type: CameraEventRecordingOn, VideoDevice: "video0"}

	evs := receiveEvents(t, coordinator.Events(), 1, defaultTimeout)

	if len(evs) != 1 {
		t.Fatalf("expected exactly one event, got %d (events: %v)", len(evs), evs)
	}

	ev := evs[0]
	assertEvent(t, ev, CameraEventRecordingOn, "video0")

	detectorB.events <- CameraEvent{Detector: "B", Type: CameraEventRecordingOn, VideoDevice: "video0"}

	// no new events should be emitted since the coordinator should ignore the second ON for the same device
	evs = receiveEvents(t, coordinator.Events(), 0, defaultTimeout)
	if len(evs) != 0 {
		t.Fatalf("expected no events, got %d (events: %v)", len(evs), evs)
	}
}

func TestCameraCoordinatorIgnoresOffWithoutPriorOn(t *testing.T) {
	// Simpler concurrency: stray OFF must be ignored and only ON->OFF be emitted.
	detector := newControllableDetector()
	coordinator := NewCameraCoordinator(detector)

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Go(func() {
		coordinator.Run(ctx)
	})
	defer func() {
		close(detector.events)
		cancel()
		wg.Wait()
	}()

	detector.events <- CameraEvent{Type: CameraEventRecordingOff, VideoDevice: "video9"}

	// no events should be emitted since the OFF is stray without a prior ON
	evs := receiveEvents(t, coordinator.Events(), 0, defaultTimeout)
	if len(evs) != 0 {
		t.Fatalf("video9: expected no events, got %d (events: %v)", len(evs), evs)
	}

	detector.events <- CameraEvent{Type: CameraEventRecordingOn, VideoDevice: "video9"}
	detector.events <- CameraEvent{Type: CameraEventRecordingOff, VideoDevice: "video9"}

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

func TestCameraCoordinatorEmitsOnlyLastOffForOverlappingOnes(t *testing.T) {
	// Simpler concurrency: multiple ONs collapse to a single ON and the
	// corresponding OFF is emitted only after the last OFF.
	detector := newControllableDetector()
	coordinator := NewCameraCoordinator(detector)

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Go(func() {
		coordinator.Run(ctx)
	})
	defer func() {
		close(detector.events)
		cancel()
		wg.Wait()
	}()

	detector.events <- CameraEvent{Type: CameraEventRecordingOn, VideoDevice: "video2"}
	detector.events <- CameraEvent{Type: CameraEventRecordingOn, VideoDevice: "video2"}

	evs := receiveEvents(t, coordinator.Events(), 1, defaultTimeout)
	if len(evs) != 1 {
		t.Fatalf("video2: expected exactly one event, got %d (events: %v)", len(evs), evs)
	}

	ev := evs[0]
	assertEvent(t, ev, CameraEventRecordingOn, "video2")

	detector.events <- CameraEvent{Type: CameraEventRecordingOff, VideoDevice: "video2"}

	// no events should be emitted since the first OFF is ignored while there's still an active ON
	evs = receiveEvents(t, coordinator.Events(), 0, defaultTimeout)
	if len(evs) != 0 {
		t.Fatalf("video2: expected no events, got %d (events: %v)", len(evs), evs)
	}

	detector.events <- CameraEvent{Type: CameraEventRecordingOff, VideoDevice: "video2"}

	evs = receiveEvents(t, coordinator.Events(), 1, defaultTimeout)
	if len(evs) != 1 {
		t.Fatalf("video2: expected exactly one event, got %d (events: %v)", len(evs), evs)
	}

	ev = evs[0]
	assertEvent(t, ev, CameraEventRecordingOff, "video2")
}

func receiveEvents(t *testing.T, ch <-chan CameraEvent, count int, timeout time.Duration) []CameraEvent {
	t.Helper()
	evs := make([]CameraEvent, 0, count)
	for i := 0; i < count; i++ {
		select {
		case ev := <-ch:
			evs = append(evs, ev)
		case <-time.After(timeout):
			t.Fatalf("timeout waiting for event %d", i)
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
