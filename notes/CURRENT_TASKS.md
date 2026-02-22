# Current Tasks

## In progress

- (none)

## Completed

- Implement `cmd/autolight`: camera-triggered Litra light controller
  - `cmd/autolight/architecture.md` — architecture documentation
  - `cmd/autolight/config.go` — `AppConfig` with `CameraNames`/`LightNames`, `LoadConfig`
  - `cmd/autolight/config_test.go` — config parsing tests
  - `cmd/autolight/litra_notifier.go` — `LitraNotifier`, injectable `litraController` interface, `defaultLitraController`
  - `cmd/autolight/litra_notifier_test.go` — 100% coverage of all logic
  - `cmd/autolight/main.go` — top-level wiring (Detector → Coordinator → Broadcaster → LitraNotifier)
