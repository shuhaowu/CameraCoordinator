# Plan

## Long-term
- Implement full `CameraCoordinator` with multi-detector merge and per-device dedupe.
- Add camera metadata lookup abstraction and Linux-backed implementation.
- Build adapters (`DBusAdapter`, `DebugAdapter`) and `EventBroadcaster` fan-out.
- Harden eBPF detector compatibility and observability.
- Add CI pipeline for formatting, linting, tests, and privileged integration checks.
