# Autolight Architecture

Autolight is a standalone program that listens for `CameraEvent` DBus signals emitted by [CameraCoordinator](../docs/development/architecture.md) and toggles Logitech Litra lights on/off in response.

It runs as a separate process alongside `camera-coordinator`. It does **not** import or depend on the `cameracoordinator` library — it communicates exclusively via DBus.

## How it works

```
┌──────────────────────┐         DBus (system bus)         ┌──────────────┐
│  camera-coordinator  │ ──── CameraEvent signal ────────► │  autolight   │
│  (emits signals)     │                                   │  (listener)  │
└──────────────────────┘                                   └──────┬───────┘
                                                                  │
                                                                  │ go-litra-driver
                                                                  ▼
                                                           ┌──────────────┐
                                                           │ Litra lights │
                                                           │   (USB HID)  │
                                                           └──────────────┘
```

### Signal flow

1. `camera-coordinator` detects a webcam starting/stopping recording and emits a `io.github.shuhaowu.CameraCoordinator.CameraEvent` signal on the system bus at object path `/io/github/shuhaowu/CameraCoordinator`.
2. Autolight subscribes to that signal via `AddMatchSignal`.
3. On each signal, autolight decodes the body (Detector, Type, VideoDevice, Card, BusInfo).
4. It checks whether the `Card` field matches the configured camera filter. If not, the event is ignored.
5. It maintains an `activeCameras` map keyed by `VideoDevice`:
   - **First camera on** (map was empty → non-empty): discover Litra devices via `lib.ListDevices()`, filter by configured light names, call `lib.LightOn()` for each match.
   - **Last camera off** (map becomes empty): same discovery, call `lib.LightOff()` for each match.
   - Otherwise: log but take no light action (lights are already in the right state).

### Why rediscover lights every time?

Camera on/off events are infrequent (a handful per day at most), so calling `lib.ListDevices()` each time is negligible. This avoids stale device indices if lights are unplugged/replugged between events.

## Files

| File | Purpose |
|------|---------|
| `main.go` | Entry point, DBus listener loop, signal handling, light control |
| `config.go` | JSON config loading and camera/light name matching helpers |

## Configuration

JSON file passed via `-config` flag. Both fields are optional; omitting them means "match everything".

```json
{
  "camera_cards": ["Logitech Webcam C930e"],
  "light_names": ["Litra Glow"]
}
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `camera_cards` | `[]string` | `[]` (all) | V4L2 card names to react to. Empty = any camera. |
| `light_names` | `[]string` | `[]` (all) | Litra device names to control. Empty = all discovered lights. |

## Future work

- Query camera exposure/white balance to auto-adjust light temperature and brightness.
