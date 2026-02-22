# Autolight Architecture

## Purpose

`autolight` is a self-contained binary that turns Litra lights on when a
configured set of cameras starts recording, and off when they all stop.  It is
built on top of the `cameracoordinator` library and adds a single custom
notifier, `LitraNotifier`, that encapsulates all light-control logic.

## Component Topology

```
┌──────────────────────────────────────────────────────────────────┐
│ CameraCoordinator                                                │
│   └─ EBPFVb2IoctlStreamDetector (hard-coded, single detector)   │
│        emits CameraEvent{VideoDevice, Capability, Type}          │
└────────────────────────┬─────────────────────────────────────────┘
                         │ Events() <-chan CameraEvent
                         ▼
               EventBroadcaster(n=1)
                         │ Channel(0)
                         ▼
                  LitraNotifier
                         │
                         ▼
            go-litra-driver/lib  (USB HID)
```

`EventBroadcaster` is used even with a single notifier to keep the topology
consistent with the rest of the codebase and to make it easy to add more
notifiers later without changing the coordinator wiring.

## LitraNotifier

### Responsibilities

- Filter incoming camera events by camera name.
- Maintain the set of currently-recording cameras that passed the filter.
- When the set transitions from empty → non-empty, turn matching lights **on**.
- When the set transitions from non-empty → empty, turn matching lights **off**.
- Filter which lights to operate on by light name.

### Camera Name Matching

`AppConfig.CameraNames` is a list of substrings.  An event's camera is
considered a match when `V4L2Capability.CardString()` contains **any** of the
configured substrings (case-sensitive).  An empty `CameraNames` list matches
every camera.

### Light Name Matching

`AppConfig.LightNames` is a list of substrings.  A discovered Litra device is
operated on if `DiscoveredDevice.Name` contains **any** of the configured
substrings (case-sensitive).  An empty `LightNames` list matches every light.

### State Tracking

`LitraNotifier.Run` is a single goroutine that reads events sequentially from
a channel, so the active-device set (`map[string]struct{}`) requires no
synchronisation.

```
RecordingOn  event for device D:
    if cameraMatches(D.Capability.CardString()):
        if len(activeDevices) == 0: turnLightsOn()
        activeDevices[D] = {}

RecordingOff event for device D:
    delete(activeDevices, D)
    if len(activeDevices) == 0: turnLightsOff()
```

`turnLightsOn` / `turnLightsOff` call `litraController.ListDevices()`, filter
the result by light name, then call `LightOn` / `LightOff` for each matching
device by its `Index`.

### litraController Interface

All calls into `go-litra-driver/lib` are made through an injectable
`litraController` interface:

```go
type litraController interface {
    ListDevices() []lib.DiscoveredDevice
    LightOn(deviceIndex int)
    LightOff(deviceIndex int)
}
```

The production implementation (`defaultLitraController`) delegates directly to
`lib.*`.  Tests supply a fake that records calls without touching hardware.

## Configuration

```jsonc
{
    // Substrings matched against V4L2 CardString (e.g. "Logitech BRIO").
    // Empty list = match any camera.
    "camera_names": ["Logitech"],

    // Substrings matched against Litra DiscoveredDevice.Name.
    // Empty list = match all lights.
    "light_names": ["Litra Glow"]
}
```

`LoadConfig` uses `json.Decoder` with `DisallowUnknownFields` to surface
typos in the config file at startup.

The default config (no file specified) matches all cameras and all lights.

## Binary Flags

| Flag        | Default | Description                              |
|-------------|---------|------------------------------------------|
| `-verbose`  | false   | Set slog level to Debug                  |
| `-config`   | ""      | Path to JSON config file; omit for defaults |

## Testability

- `LitraNotifier` accepts a `litraController` so tests never touch USB hardware.
- Config loading is tested separately against known JSON strings.
- `main.go` wiring is not unit-tested (standard pattern in this codebase).
