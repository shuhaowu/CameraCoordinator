package main

import (
	"context"
	"sync"
	"testing"
	"time"

	lib "github.com/kharyam/go-litra-driver/lib"
	"github.com/shuhaowu/cameracoordinator"
)

const defaultTimeout = 50 * time.Millisecond

// fakeLitraController records LightOn/LightOff calls without touching hardware.
type fakeLitraController struct {
	mu      sync.Mutex
	devices []lib.DiscoveredDevice
	calls   []fakeCall
}

type fakeCall struct {
	op    string // "on" or "off"
	index int
}

func (f *fakeLitraController) ListDevices() []lib.DiscoveredDevice {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.devices
}

func (f *fakeLitraController) LightOn(deviceIndex int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, fakeCall{"on", deviceIndex})
}

func (f *fakeLitraController) LightOff(deviceIndex int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, fakeCall{"off", deviceIndex})
}

func (f *fakeLitraController) drainCalls() []fakeCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := f.calls
	f.calls = nil
	return c
}

// makeCapability builds a V4L2Capability whose CardString returns card.
func makeCapability(card string) cameracoordinator.V4L2Capability {
	var cap cameracoordinator.V4L2Capability
	copy(cap.Card[:], []byte(card))
	return cap
}

// sendEvent sends an event to events with a timeout to prevent test hangs.
func sendEvent(t *testing.T, events chan<- cameracoordinator.CameraEvent, ev cameracoordinator.CameraEvent) {
	t.Helper()
	select {
	case events <- ev:
	case <-time.After(defaultTimeout):
		t.Fatal("timed out sending event")
	}
}

// runNotifier starts n.Run in a goroutine and returns a cancel function plus a
// WaitGroup so the caller can stop and join cleanly.
func runNotifier(n *LitraNotifier, events <-chan cameracoordinator.CameraEvent) (context.CancelFunc, *sync.WaitGroup) {
	ctx, cancel := context.WithCancel(context.Background())
	wg := &sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = n.Run(ctx, events)
	}()
	return cancel, wg
}

// waitForCalls polls until at least n calls are recorded or the timeout elapses.
func waitForCalls(t *testing.T, ctrl *fakeLitraController, want int) []fakeCall {
	t.Helper()
	deadline := time.Now().Add(defaultTimeout)
	for time.Now().Before(deadline) {
		ctrl.mu.Lock()
		n := len(ctrl.calls)
		ctrl.mu.Unlock()
		if n >= want {
			break
		}
		time.Sleep(time.Millisecond)
	}
	return ctrl.drainCalls()
}

// TestLitraNotifier_CameraOnTurnsLightsOn verifies that a RecordingOn event
// for a matching camera triggers LightOn for all matching lights.
func TestLitraNotifier_CameraOnTurnsLightsOn(t *testing.T) {
	ctrl := &fakeLitraController{
		devices: []lib.DiscoveredDevice{
			{Index: 1, Name: "Litra Glow"},
		},
	}
	n := NewLitraNotifier(AppConfig{}, ctrl)
	events := make(chan cameracoordinator.CameraEvent, 1)
	cancel, wg := runNotifier(n, events)
	defer wg.Wait()
	defer cancel()

	sendEvent(t, events, cameracoordinator.CameraEvent{
		Type:        cameracoordinator.CameraEventRecordingOn,
		VideoDevice: "video0",
		Capability:  makeCapability("Logitech BRIO"),
	})

	calls := waitForCalls(t, ctrl, 1)
	if len(calls) != 1 || calls[0].op != "on" || calls[0].index != 1 {
		t.Errorf("expected LightOn(1), got %+v", calls)
	}
}

// TestLitraNotifier_CameraOffTurnsLightsOff verifies that a RecordingOff event
// after a RecordingOn triggers LightOff for matching lights.
func TestLitraNotifier_CameraOffTurnsLightsOff(t *testing.T) {
	ctrl := &fakeLitraController{
		devices: []lib.DiscoveredDevice{
			{Index: 2, Name: "Litra Beam"},
		},
	}
	n := NewLitraNotifier(AppConfig{}, ctrl)
	events := make(chan cameracoordinator.CameraEvent, 2)
	cancel, wg := runNotifier(n, events)
	defer wg.Wait()
	defer cancel()

	sendEvent(t, events, cameracoordinator.CameraEvent{
		Type:        cameracoordinator.CameraEventRecordingOn,
		VideoDevice: "video0",
		Capability:  makeCapability("Any Camera"),
	})
	waitForCalls(t, ctrl, 1) // consume the on call

	sendEvent(t, events, cameracoordinator.CameraEvent{
		Type:        cameracoordinator.CameraEventRecordingOff,
		VideoDevice: "video0",
	})

	calls := waitForCalls(t, ctrl, 1)
	if len(calls) != 1 || calls[0].op != "off" || calls[0].index != 2 {
		t.Errorf("expected LightOff(2), got %+v", calls)
	}
}

// TestLitraNotifier_LightsStayOnWithMultipleCameras verifies that lights
// remain on when one of two active cameras stops recording. Only after the
// last active camera stops should the lights turn off.
func TestLitraNotifier_LightsStayOnWithMultipleCameras(t *testing.T) {
	ctrl := &fakeLitraController{
		devices: []lib.DiscoveredDevice{
			{Index: 1, Name: "Litra Glow"},
		},
	}
	n := NewLitraNotifier(AppConfig{}, ctrl)
	events := make(chan cameracoordinator.CameraEvent, 4)
	cancel, wg := runNotifier(n, events)
	defer wg.Wait()
	defer cancel()

	// Start two cameras.
	sendEvent(t, events, cameracoordinator.CameraEvent{
		Type:        cameracoordinator.CameraEventRecordingOn,
		VideoDevice: "video0",
		Capability:  makeCapability("Cam A"),
	})
	waitForCalls(t, ctrl, 1) // lights on

	sendEvent(t, events, cameracoordinator.CameraEvent{
		Type:        cameracoordinator.CameraEventRecordingOn,
		VideoDevice: "video1",
		Capability:  makeCapability("Cam B"),
	})

	// Stop the first camera — lights must stay on.
	sendEvent(t, events, cameracoordinator.CameraEvent{
		Type:        cameracoordinator.CameraEventRecordingOff,
		VideoDevice: "video0",
	})

	// Give the notifier time to process and verify no off call arrived.
	time.Sleep(defaultTimeout)
	calls := ctrl.drainCalls()
	for _, c := range calls {
		if c.op == "off" {
			t.Errorf("lights turned off while second camera still active: %+v", calls)
		}
	}

	// Stop the second camera — now lights should turn off.
	sendEvent(t, events, cameracoordinator.CameraEvent{
		Type:        cameracoordinator.CameraEventRecordingOff,
		VideoDevice: "video1",
	})
	calls = waitForCalls(t, ctrl, 1)
	if len(calls) != 1 || calls[0].op != "off" {
		t.Errorf("expected LightOff after last camera stopped, got %+v", calls)
	}
}

// TestLitraNotifier_FilterExcludesNonMatchingCamera verifies that a camera
// whose CardString does not match the configured filter is ignored entirely,
// so no light calls are made.
func TestLitraNotifier_FilterExcludesNonMatchingCamera(t *testing.T) {
	ctrl := &fakeLitraController{
		devices: []lib.DiscoveredDevice{
			{Index: 1, Name: "Litra Glow"},
		},
	}
	n := NewLitraNotifier(AppConfig{CameraNames: []string{"Logitech"}}, ctrl)
	events := make(chan cameracoordinator.CameraEvent, 2)
	cancel, wg := runNotifier(n, events)
	defer wg.Wait()
	defer cancel()

	// Camera whose name does not contain "Logitech".
	sendEvent(t, events, cameracoordinator.CameraEvent{
		Type:        cameracoordinator.CameraEventRecordingOn,
		VideoDevice: "video0",
		Capability:  makeCapability("Razer Kiyo"),
	})
	sendEvent(t, events, cameracoordinator.CameraEvent{
		Type:        cameracoordinator.CameraEventRecordingOff,
		VideoDevice: "video0",
	})

	// Allow enough time for events to process.
	time.Sleep(defaultTimeout)
	calls := ctrl.drainCalls()
	if len(calls) != 0 {
		t.Errorf("expected no light calls for non-matching camera, got %+v", calls)
	}
}

// TestLitraNotifier_FilterExcludesNonMatchingLight verifies that lights whose
// name does not match the light filter are left untouched.
func TestLitraNotifier_FilterExcludesNonMatchingLight(t *testing.T) {
	ctrl := &fakeLitraController{
		devices: []lib.DiscoveredDevice{
			{Index: 1, Name: "Litra Glow"},
			{Index: 2, Name: "Some Other Light"},
		},
	}
	n := NewLitraNotifier(AppConfig{LightNames: []string{"Litra Glow"}}, ctrl)
	events := make(chan cameracoordinator.CameraEvent, 1)
	cancel, wg := runNotifier(n, events)
	defer wg.Wait()
	defer cancel()

	sendEvent(t, events, cameracoordinator.CameraEvent{
		Type:        cameracoordinator.CameraEventRecordingOn,
		VideoDevice: "video0",
		Capability:  makeCapability("Any Camera"),
	})

	calls := waitForCalls(t, ctrl, 1)
	// Only the matching light should have been operated on.
	if len(calls) != 1 || calls[0].index != 1 {
		t.Errorf("expected only light index 1 to be turned on, got %+v", calls)
	}
}

// TestLitraNotifier_EmptyFiltersMatchEverything confirms that zero-length
// CameraNames and LightNames slices match all devices (the default behaviour).
func TestLitraNotifier_EmptyFiltersMatchEverything(t *testing.T) {
	ctrl := &fakeLitraController{
		devices: []lib.DiscoveredDevice{
			{Index: 1, Name: "Light A"},
			{Index: 2, Name: "Light B"},
		},
	}
	n := NewLitraNotifier(AppConfig{}, ctrl) // empty config
	events := make(chan cameracoordinator.CameraEvent, 1)
	cancel, wg := runNotifier(n, events)
	defer wg.Wait()
	defer cancel()

	sendEvent(t, events, cameracoordinator.CameraEvent{
		Type:        cameracoordinator.CameraEventRecordingOn,
		VideoDevice: "video0",
		Capability:  makeCapability("Any Camera"),
	})

	calls := waitForCalls(t, ctrl, 2)
	if len(calls) != 2 {
		t.Errorf("expected 2 LightOn calls for all lights, got %+v", calls)
	}
}

// TestLitraNotifier_ContextCancellation confirms that Run returns nil when the
// context is cancelled, even if the events channel remains open.
func TestLitraNotifier_ContextCancellation(t *testing.T) {
	ctrl := &fakeLitraController{}
	n := NewLitraNotifier(AppConfig{}, ctrl)
	events := make(chan cameracoordinator.CameraEvent) // never closed
	cancel, wg := runNotifier(n, events)

	cancel()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(defaultTimeout * 10):
		t.Fatal("Run did not return after context cancellation")
	}
}

// TestLitraNotifier_ClosedChannel confirms that Run returns nil when the
// events channel is closed, even if the context is still live.
func TestLitraNotifier_ClosedChannel(t *testing.T) {
	ctrl := &fakeLitraController{}
	n := NewLitraNotifier(AppConfig{}, ctrl)
	events := make(chan cameracoordinator.CameraEvent)
	cancel, wg := runNotifier(n, events)
	defer cancel()

	close(events)

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(defaultTimeout * 10):
		t.Fatal("Run did not return after events channel was closed")
	}
}

// TestLitraNotifier_CameraNameFilterMatchesSubstring verifies that a camera
// whose CardString contains a configured substring (not just an exact match)
// is correctly treated as matching and triggers light operations.
func TestLitraNotifier_CameraNameFilterMatchesSubstring(t *testing.T) {
	ctrl := &fakeLitraController{
		devices: []lib.DiscoveredDevice{
			{Index: 1, Name: "Litra Glow"},
		},
	}
	// Filter uses "Logitech"; camera card is "Logitech BRIO" which contains it.
	n := NewLitraNotifier(AppConfig{CameraNames: []string{"Logitech"}}, ctrl)
	events := make(chan cameracoordinator.CameraEvent, 1)
	cancel, wg := runNotifier(n, events)
	defer wg.Wait()
	defer cancel()

	sendEvent(t, events, cameracoordinator.CameraEvent{
		Type:        cameracoordinator.CameraEventRecordingOn,
		VideoDevice: "video0",
		Capability:  makeCapability("Logitech BRIO"),
	})

	calls := waitForCalls(t, ctrl, 1)
	if len(calls) != 1 || calls[0].op != "on" {
		t.Errorf("expected LightOn for matching camera substring, got %+v", calls)
	}
}

// TestLitraNotifier_UnknownEventType ensures that an event with an unrecognised
// type is silently ignored without triggering any light operations or panicking.
func TestLitraNotifier_UnknownEventType(t *testing.T) {
	ctrl := &fakeLitraController{
		devices: []lib.DiscoveredDevice{
			{Index: 1, Name: "Litra Glow"},
		},
	}
	n := NewLitraNotifier(AppConfig{}, ctrl)
	events := make(chan cameracoordinator.CameraEvent, 1)
	cancel, wg := runNotifier(n, events)
	defer wg.Wait()
	defer cancel()

	// Send an event with a type value that is not RecordingOn or RecordingOff.
	sendEvent(t, events, cameracoordinator.CameraEvent{
		Type:        cameracoordinator.CameraEventType(99),
		VideoDevice: "video0",
	})

	time.Sleep(defaultTimeout)
	calls := ctrl.drainCalls()
	if len(calls) != 0 {
		t.Errorf("expected no light calls for unknown event type, got %+v", calls)
	}
}

// TestLitraNotifier_RecordingOffWithNoActiveDevices ensures that a
// RecordingOff event for a device that was not previously tracked (e.g.
// because it was filtered out, or the on event was missed) does not trigger
// any light operations and does not panic.
func TestLitraNotifier_RecordingOffWithNoActiveDevices(t *testing.T) {
	ctrl := &fakeLitraController{
		devices: []lib.DiscoveredDevice{
			{Index: 1, Name: "Litra Glow"},
		},
	}
	n := NewLitraNotifier(AppConfig{}, ctrl)
	events := make(chan cameracoordinator.CameraEvent, 1)
	cancel, wg := runNotifier(n, events)
	defer wg.Wait()
	defer cancel()

	// Send off event without a prior on event.
	sendEvent(t, events, cameracoordinator.CameraEvent{
		Type:        cameracoordinator.CameraEventRecordingOff,
		VideoDevice: "video0",
	})

	time.Sleep(defaultTimeout)
	calls := ctrl.drainCalls()
	if len(calls) != 0 {
		t.Errorf("expected no light calls for spurious off event, got %+v", calls)
	}
}
