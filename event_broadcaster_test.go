package cameracoordinator

import (
	"context"
	"sync"
	"testing"
	"time"
)

// collectN reads exactly n events from ch, blocking until all arrive or until
// the timeout expires. If the timeout fires before n events are received it
// calls t.Fatalf, giving the caller's context via msg.
func collectN(t *testing.T, ch <-chan CameraEvent, n int, timeout time.Duration, msg string) []CameraEvent {
	t.Helper()
	result := make([]CameraEvent, 0, n)
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for i := 0; i < n; i++ {
		select {
		case ev, ok := <-ch:
			if !ok {
				t.Fatalf("%s: channel closed after %d/%d events", msg, i, n)
			}
			result = append(result, ev)
		case <-timer.C:
			t.Fatalf("%s: timed out waiting for event %d/%d", msg, i+1, n)
		}
	}
	return result
}

// assertChannelClosed waits until ch is closed (or a short deadline expires).
func assertChannelClosed(t *testing.T, ch <-chan CameraEvent, timeout time.Duration, msg string) {
	t.Helper()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatalf("%s: expected channel to be closed but received an event", msg)
		}
	case <-timer.C:
		t.Fatalf("%s: timed out waiting for channel to close", msg)
	}
}

const eventTimeout = 100 * time.Millisecond

// TestEventBroadcaster_BasicFanOut verifies that a single event sent to the
// source is forwarded to all output channels exactly once.
//
// All output channels are consumed concurrently to avoid blocking the
// broadcaster, which sends to each output in turn before advancing.
func TestEventBroadcaster_BasicFanOut(t *testing.T) {
	const numOutputs = 3
	source := make(chan CameraEvent, 1)
	b := NewEventBroadcaster(numOutputs, 0)

	ctx, cancel := context.WithCancel(context.Background())

	var runWg sync.WaitGroup
	runWg.Go(func() {
		b.Run(ctx, source)
	})
	defer runWg.Wait()
	defer cancel()

	want := CameraEvent{Detector: "test", Type: CameraEventRecordingOn, VideoDevice: "video0"}
	source <- want

	// All output channels must be consumed concurrently; the broadcaster blocks
	// on output[i+1] until output[i] has been read for a given event.
	results := make([]CameraEvent, numOutputs)
	var collectWg sync.WaitGroup
	for i := range numOutputs {
		collectWg.Go(func() {
			got := collectN(t, b.Channel(i), 1, eventTimeout, "output channel")
			results[i] = got[0]
		})
	}
	collectWg.Wait()

	for i, got := range results {
		if got != want {
			t.Errorf("output[%d]: got %+v, want %+v", i, got, want)
		}
	}
}

// TestEventBroadcaster_MultipleEvents verifies that a sequence of events is
// delivered to all adapters in the same order as they were sent.
//
// The broadcaster delivers each event to all outputs before moving to the next,
// so consumers must read concurrently to avoid deadlocking the broadcaster.
func TestEventBroadcaster_MultipleEvents(t *testing.T) {
	events := []CameraEvent{
		{Detector: "d1", Type: CameraEventRecordingOn, VideoDevice: "video0"},
		{Detector: "d1", Type: CameraEventRecordingOff, VideoDevice: "video0"},
		{Detector: "d1", Type: CameraEventRecordingOn, VideoDevice: "video1"},
	}

	const numOutputs = 2
	source := make(chan CameraEvent, len(events))
	b := NewEventBroadcaster(numOutputs, 0)

	ctx, cancel := context.WithCancel(context.Background())

	var runWg sync.WaitGroup
	runWg.Go(func() {
		b.Run(ctx, source) //nolint:errcheck
	})
	defer runWg.Wait()
	defer cancel()

	for _, ev := range events {
		source <- ev
	}

	// Collect from all output channels concurrently: because the broadcaster
	// sends event-by-event to all outputs before advancing, a sequential reader
	// would block the broadcaster at output[1] while waiting on output[0].
	results := make([][]CameraEvent, numOutputs)
	var collectWg sync.WaitGroup
	for i := range numOutputs {
		collectWg.Go(func() {
			results[i] = collectN(t, b.Channel(i), len(events), eventTimeout, "output channel")
		})
	}
	collectWg.Wait()

	for i, got := range results {
		for j, want := range events {
			if got[j] != want {
				t.Errorf("output[%d] event[%d]: got %+v, want %+v", i, j, got[j], want)
			}
		}
	}
}

// TestEventBroadcaster_SourceClosedClosesOutputs verifies that when the source
// channel is closed, Run exits and all output channels are subsequently closed.
// Adapters depend on this to detect "no more events" without context awareness.
func TestEventBroadcaster_SourceClosedClosesOutputs(t *testing.T) {
	source := make(chan CameraEvent)
	b := NewEventBroadcaster(2, 0)

	var wg sync.WaitGroup
	wg.Go(func() {
		b.Run(context.Background(), source)
	})

	close(source)

	for i := 0; i < 2; i++ {
		assertChannelClosed(t, b.Channel(i), eventTimeout, "output channel after source close")
	}

	wg.Wait()
}

// TestEventBroadcaster_ContextCancelClosesOutputs verifies that cancelling ctx
// causes Run to return and all output channels to be closed, even if the source
// channel is never closed. This simulates a graceful shutdown scenario.
func TestEventBroadcaster_ContextCancelClosesOutputs(t *testing.T) {
	source := make(chan CameraEvent) // never closed intentionally
	b := NewEventBroadcaster(2, 0)

	ctx, cancel := context.WithCancel(context.Background())

	var wg sync.WaitGroup
	wg.Go(func() {
		b.Run(ctx, source)
	})

	cancel()

	for i := 0; i < 2; i++ {
		assertChannelClosed(t, b.Channel(i), eventTimeout, "output channel after context cancel")
	}

	wg.Wait()
}

// TestEventBroadcaster_ZeroOutputs verifies that the broadcaster works
// correctly when there are no output channels: events are consumed from the
// source without blocking, and Run exits cleanly when the source closes.
func TestEventBroadcaster_ZeroOutputs(t *testing.T) {
	source := make(chan CameraEvent, 1)
	b := NewEventBroadcaster(0, 0)

	var wg sync.WaitGroup
	wg.Go(func() {
		b.Run(context.Background(), source) //nolint:errcheck
	})

	// Send an event and then close. With zero outputs the Run loop should not
	// block here, so we verify it terminates cleanly.
	source <- CameraEvent{Detector: "d", Type: CameraEventRecordingOn, VideoDevice: "video0"}
	close(source)

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// success: Run returned
	case <-time.After(eventTimeout):
		t.Fatal("Run did not return after source closed with zero outputs")
	}
}

// TestEventBroadcaster_RunCalledTwice_IndependentInstances verifies that two
// independent EventBroadcaster instances do not interfere with each other, as
// Run closes its own output channels only.
func TestEventBroadcaster_IndependentInstances(t *testing.T) {
	src1 := make(chan CameraEvent, 1)
	src2 := make(chan CameraEvent, 1)
	b1 := NewEventBroadcaster(1, 0)
	b2 := NewEventBroadcaster(1, 0)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	wg.Go(func() { b1.Run(ctx, src1) })
	wg.Go(func() { b2.Run(ctx, src2) })

	ev1 := CameraEvent{Detector: "d1", Type: CameraEventRecordingOn, VideoDevice: "video0"}
	ev2 := CameraEvent{Detector: "d2", Type: CameraEventRecordingOff, VideoDevice: "video1"}

	src1 <- ev1
	src2 <- ev2

	got1 := collectN(t, b1.Channel(0), 1, eventTimeout, "b1 output")
	got2 := collectN(t, b2.Channel(0), 1, eventTimeout, "b2 output")

	if got1[0] != ev1 {
		t.Errorf("b1: got %+v, want %+v", got1[0], ev1)
	}
	if got2[0] != ev2 {
		t.Errorf("b2: got %+v, want %+v", got2[0], ev2)
	}

	cancel()
	wg.Wait()
}
