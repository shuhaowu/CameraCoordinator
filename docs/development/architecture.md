# Architecture

CameraCoordinator is a Golang service that detects if webcams plugged into a Linux machine is turned on or off. Interally, it consists of the following components:

- A `CameraDetector` (camera_detector.go) interface that detects when the webcam is turned on/off. The events are emitted via an event channel.
  - Multiple implementations of this interface (camera_detector_*.go) can exist for different method.
  - A `CameraCoordinator` (camera_coordinator.go) struct implements the interface and merges multiple `CameraDetectors` into a single one. If multiple detection triggers (in a sequence of on, on, on or off, off, off), it will only trigger on the first event for that video device.
  - The main detection method is implemented via BPF using the `github.com/cilium/ebpf` library.
- The library also implement methods (in `v4l2_utils.go`) to query the camera human readable name and other metadata given a video device filename using the VIDIOC_QUERYCAP ioctl.
- Multiple Adapters that adapts the events from the CameraCoordinator to various sinks:
  - A `DBusAdapter` that takes the events from the channel and convert it to dbus messages.
  - A `DebugAdapter` that
- A `EventBroadcaster` that can mirror the event from a single events channel (from the `CameraCoordinator`) to multiple events channels (to multiple adapters).

Camera Events
-------------

The `CameraEvent` is defined as follows:

```go

const (
  CameraEventRecordingOn CameraEventType = iota
  CameraEventRecordingOff
)

struct CameraEvent {
  // The event type
  Type CameraEventType

  // The filename of the video device detected from
  VideoFilename string
}

```
