package cameracoordinator

import (
	"context"
	"log/slog"
)

// Compile-time check that *EventBroadcaster implements Adapter.
var _ Adapter = (*EventBroadcaster)(nil)

// EventBroadcaster fans out camera events from a single coordinator source
// channel to a fixed set of adapter channels. Each output channel is created
// by the broadcaster and can be retrieved via Channel.
//
// EventBroadcaster implements the Adapter interface: Run accepts the source
// events channel at call time. It blocks until either the source channel is
// closed or ctx is cancelled. When Run returns, all output channels are closed
// so that adapter goroutines can detect the shutdown naturally.
type EventBroadcaster struct {
	outputs []chan CameraEvent
}

// NewEventBroadcaster creates an EventBroadcaster that forwards events to n
// independent output channels. Panics if n is negative.
func NewEventBroadcaster(n int, bufSize int) *EventBroadcaster {
	if n < 0 {
		panic("event broadcaster: n must be non-negative")
	}

	outputs := make([]chan CameraEvent, n)
	for i := range outputs {
		outputs[i] = make(chan CameraEvent, bufSize)
	}
	return &EventBroadcaster{outputs: outputs}
}

// Channel returns the i-th output channel (0-indexed) as a receive-only
// channel suitable for passing to an Adapter.
func (b *EventBroadcaster) Channel(i int) <-chan CameraEvent {
	return b.outputs[i]
}

// Run reads events from events and copies each event to every output channel
// in order. It implements the Adapter interface. It returns nil when events is
// closed or ctx is cancelled. All output channels are closed before Run returns.
func (b *EventBroadcaster) Run(ctx context.Context, events <-chan CameraEvent) error {
	logger := slog.With("component", "EventBroadcaster")

	defer func() {
		for _, ch := range b.outputs {
			close(ch)
		}
		logger.Debug("all output channels closed")
	}()

	for {
		select {
		case <-ctx.Done():
			logger.Debug("context cancelled, stopping broadcaster")
			return nil
		case ev, ok := <-events:
			if !ok {
				logger.Debug("source channel closed, stopping broadcaster")
				return nil
			}

			logger.Debug("broadcasting event",
				"type", ev.Type.String(),
				"video_device", ev.VideoDevice,
				"detector", ev.Detector,
				"num_outputs", len(b.outputs),
			)

			for _, out := range b.outputs {
				select {
				case <-ctx.Done():
					return nil
				case out <- ev:
				}
			}
		}
	}
}
