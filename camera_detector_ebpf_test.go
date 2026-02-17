package cameracoordinator

import (
	"testing"
)

func TestNormalizeVideoFilename(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "basename to dev path", in: "video0", want: "/dev/video0"},
		{name: "already normalized", in: "/dev/video1", want: "/dev/video1"},
		{name: "trim whitespace", in: "  video2  ", want: "/dev/video2"},
		{name: "empty stays empty", in: "   ", want: ""},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := normalizeVideoFilename(test.in); got != test.want {
				t.Fatalf("normalizeVideoFilename(%q) = %q, want %q", test.in, got, test.want)
			}
		})
	}
}

func TestMapRawEventToCameraEvent(t *testing.T) {
	t.Parallel()

	t.Run("stream on event maps", func(t *testing.T) {
		t.Parallel()
		// Stream-on must map to CameraEventRecordingOn to preserve architecture event contract.
		raw := ebpfRawEvent{EventType: 1}
		copy(raw.Name[:], []byte("video4"))

		event, ok := mapRawEventToCameraEvent(raw)
		if !ok {
			t.Fatal("mapRawEventToCameraEvent returned ok=false for stream-on event")
		}
		if event.Type != CameraEventRecordingOn {
			t.Fatalf("event type = %v, want %v", event.Type, CameraEventRecordingOn)
		}
		if event.VideoFilename != "/dev/video4" {
			t.Fatalf("video filename = %q, want %q", event.VideoFilename, "/dev/video4")
		}
	})

	t.Run("stream off event maps", func(t *testing.T) {
		t.Parallel()
		// Stream-off must map to CameraEventRecordingOff so downstream adapters can emit stop state.
		raw := ebpfRawEvent{EventType: 2}
		copy(raw.Name[:], []byte("video9"))

		event, ok := mapRawEventToCameraEvent(raw)
		if !ok {
			t.Fatal("mapRawEventToCameraEvent returned ok=false for stream-off event")
		}
		if event.Type != CameraEventRecordingOff {
			t.Fatalf("event type = %v, want %v", event.Type, CameraEventRecordingOff)
		}
		if event.VideoFilename != "/dev/video9" {
			t.Fatalf("video filename = %q, want %q", event.VideoFilename, "/dev/video9")
		}
	})

	t.Run("unknown event type rejected", func(t *testing.T) {
		t.Parallel()
		// Unknown event codes should be dropped to avoid emitting invalid domain events.
		raw := ebpfRawEvent{EventType: 99}
		copy(raw.Name[:], []byte("video0"))

		if _, ok := mapRawEventToCameraEvent(raw); ok {
			t.Fatal("mapRawEventToCameraEvent returned ok=true for unknown event type")
		}
	})

	t.Run("empty name rejected", func(t *testing.T) {
		t.Parallel()
		// Empty camera names indicate failed kernel extraction and must not surface as /dev/.
		raw := ebpfRawEvent{EventType: 1}

		if _, ok := mapRawEventToCameraEvent(raw); ok {
			t.Fatal("mapRawEventToCameraEvent returned ok=true for empty name")
		}
	})
}
