# Code Review

## Overall Comment

### Description

CameraCoordinator is a Go library/daemon that detects when a webcam starts or stops recording on Linux. The detection path is:

1. One or more `CameraDetector` implementations (currently one: an eBPF kprobe-based detector that hooks `vb2_ioctl_streamon`, `vb2_ioctl_streamoff`, and `vb2_fop_release`) emit raw `CameraEvent` values on private channels.
2. A `CameraCoordinator` owns those detectors, fans their channels into a single internal `allEvents` channel via per-detector forwarder goroutines, and runs a stateful event-handler goroutine. The handler tracks "how many distinct detectors are currently active per device" and emits a deduplicated ON on the first active detector and a deduplicated OFF when the last one goes inactive.
3. The coordinator's public `Events()` channel is consumed by an `EventBroadcaster`, which fans the events out to N output channels, one per adapter.
4. Each adapter (`PrintAdapter`, `ScriptAdapter`) reads from its output channel and acts on events.

This is a clean, layered architecture and the Go concurrency model is used appropriately throughout. Test coverage is good and tests are generally readable.

### Summary of Findings

The code is in solid shape for a library this size. The main areas of concern, roughly in priority order, are:

1. **`ScriptAdapter`'s background goroutines are untracked** — scripts launched with `go s.handle(...)` have no `sync.WaitGroup`, so when `Run` returns (e.g. on context cancellation), in-flight script processes may be abandoned with no way for the caller to wait for them or clean up. This is the most actionable bug.
3. **`EventBroadcaster` can deliver an event to some adapters but not others on context cancellation** — the fan-out loop iterates outputs sequentially; if the context is cancelled mid-iteration, only the outputs that came before the cancellation win receive the event. This creates a divergence in state between adapters.
4. **PID-liveness logic in the eBPF detector is correct but written in a confusing doubly-inverted style** that makes it easy to misread as the opposite of what it does.
5. **`VIDIOC_QUERYCAP` is a hardcoded hex constant** with a comment saying it was "AI generated and it seems to work". The existing struct-size test partially validates it, but the constant will silently be wrong on non-x86 architectures with no compiler or linker error.
6. **Architecture documentation (`docs/development/architecture.md`) is stale** — it references a `DBusAdapter`, a `DebugAdapter`, and a file `v4l2_utils.go` that do not exist, and uses the wrong iota values for `CameraEventType`.

---

## Inline Comments

---

### `adapter_script.go`


#### #2 — Untracked background goroutines in `Run` (line ~45)

```go
go s.handle(ctx, event)
```

`handle` is dispatched as a fire-and-forget goroutine with no `sync.WaitGroup`. When `Run` returns (either from context cancellation or channel close), any scripts currently executing in `handle` goroutines continue running in the background. The caller has no way to wait for them.

This matters in two real scenarios:
- **Graceful shutdown**: the main process wants to wait for all scripts to finish before exiting, but it cannot because `Run`'s return is the only signal it has.
- **Resource management**: a script for a previous event that is still running may conflict with a script for a new event, and there is no way to serialize or gate them.

**Fix:** Add a `sync.WaitGroup` to `ScriptAdapter`, call `wg.Add(1)` before launching each goroutine, and call `wg.Done()` at the end of `handle`. Either return the WaitGroup or expose a `Wait()` method, and call `wg.Wait()` before `Run` returns.

---

### `camera_coordinator.go`

#### #3 — `emitEvent` silently drops events on context cancellation (line ~194)

```go
func (c *CameraCoordinator) emitEvent(ctx context.Context, ev CameraEvent) {
    select {
    case <-ctx.Done():
        return
    case c.events <- ev:
    }
}
```

When the context is cancelled, this function discards the event without sending it. Because this is called from the single event-handler goroutine, a very late cancellation (e.g. after the detector fires a batch of events) can silently drop events. There is a TODO acknowledging this, but it deserves a log line at `Debug` level so that during troubleshooting it is visible that events were discarded. As it stands, the event disappears with no trace.

**Suggestion:** At minimum, add `logger.Debug("dropping event due to context cancellation", "type", ev.Type, "device", ev.VideoDevice)`.

---

#### #4 — `forwarderWg` goroutine may block forever on slow event-handler (lines ~62–88)

```go
forwarderWg.Go(func() {
    for {
        select {
        case <-ctx.Done():
            return
        case ev, ok := <-det.Events():
            ...
            select {
            case <-ctx.Done():
                return
            case allEvents <- ev:
            }
        }
    }
})
```

The inner select sends to `allEvents`, which the event handler reads. If the event handler is behind (e.g. it is inside `emitEvent` blocked on `c.events <- ev`) and the context is cancelled, the forwarder's inner `select` will pick `ctx.Done()` and return. This is fine. However, if `ctx` has _not_ yet been cancelled (e.g. a detector dies naturally without cancelling the context), then after `detectorWg.Wait()`, `cancel()` is called explicitly. At that exact moment the forwarder may be mid-send on `allEvents` with the event handler not reading. Since both check `ctx.Done()`, they will unblock once `cancel()` fires. This is correct, but the code has two separate `select` blocks that need to be analysed together to verify this — it is a subtle invariant. A comment explaining why the double-select pattern is safe would help the reader.

---

#### #5 — Inconsistent `Detector` field on emitted coordinator events (lines ~126, ~150)

When emitting `CameraEventRecordingOn`:
```go
c.emitEvent(ctx, CameraEvent{Detector: "coordinator", Type: CameraEventRecordingOn, VideoDevice: ev.VideoDevice})
```

But when emitting `CameraEventRecordingOff`:
```go
c.emitEvent(ctx, CameraEvent{Detector: ev.Detector, Type: CameraEventRecordingOff, VideoDevice: ev.VideoDevice})
```

The `Detector` field is `"coordinator"` for ON events but is set to the _underlying_ detector's name for OFF events. This inconsistency makes it impossible to write filtering code that treats all coordinator events uniformly. It will also confuse log readers who see inconsistent `detector=` values for the two halves of the same camera session.

**Fix:** Use `"coordinator"` consistently for both kinds of emitted event.

---

### `event_broadcaster.go`

#### #6 — Partial event delivery when context is cancelled mid-fan-out (line ~70)

```go
for _, out := range b.outputs {
    select {
    case <-ctx.Done():
        return nil
    case out <- ev:
    }
}
```

When the context is cancelled while iterating over `b.outputs`, the broadcaster exits immediately. Outputs that were processed earlier in the slice received this event; outputs later in the slice did not. If adapters maintain any stateful interpretation of events (e.g. the coordinator's reference-count logic or future adapters that track "is the camera on?"), diverging their views at shutdown can create inconsistent state.

This is a hard problem to solve perfectly at shutdown, but the partial delivery is not documented and a reader would not notice without careful analysis. At minimum, the behaviour should be described in a comment. An alternative is to finish delivering the current event to all outputs before checking `ctx.Done()` (i.e., move the `ctx.Done()` check outside the per-output inner loop).

---

#### #7 — `NewEventBroadcaster` panics on negative `n` but not on out-of-range `Channel(i)` (line ~27, ~42)

```go
func NewEventBroadcaster(n int, bufSize int) *EventBroadcaster {
    if n < 0 {
        panic("event broadcaster: n must be non-negative")
    }
    ...
}

func (b *EventBroadcaster) Channel(i int) <-chan CameraEvent {
    return b.outputs[i]
}
```

`NewEventBroadcaster` validates _n_ but `Channel` does no bounds checking — it will panic with an opaque index-out-of-range runtime error if `i >= n`. Since the error message from the constructor is deliberately descriptive, the `Channel` method should mirror that quality:

```go
func (b *EventBroadcaster) Channel(i int) <-chan CameraEvent {
    if i < 0 || i >= len(b.outputs) {
        panic(fmt.Sprintf("event broadcaster: channel index %d out of range [0, %d)", i, len(b.outputs)))
    }
    return b.outputs[i]
}
```

---

### `camera_detector_vb2_ioctl.go`

#### #8 — PID-liveness check is written with inverted conditions that obscure intent (lines ~132–148)

```go
if err := unix.Kill(int(ev.Pid), 0); err != nil {
    if err != unix.ESRCH {
        d.logger.Warn("failed to check pid liveness", ...)
        continue
    }
} else {
    d.logger.Debug("fop_release event PID still alive", ...)
    continue
}
```

The intent is: "skip this event if the process is still alive; emit it if the process is dead". But to understand that, the reader must mentally trace three cases (`nil`, `ESRCH`, other) through a double-negation structure. The two `continue` paths are the "skip" cases, but they are separated by the "fall through" (emit) path, which is invisible in the code — it is the absence of a `continue`.

**Fix:** Rewrite to state the positive condition explicitly:

```go
alive, err := unix.Kill(int(ev.Pid), 0)
switch {
case err == unix.ESRCH:
    // Process is gone — the fop_release is from termination; emit the event.
case err == nil:
    // Process still exists; ignore this fop_release (e.g. re-open for capability probe).
    d.logger.Debug("fop_release: PID still alive, ignoring", "pid", ev.Pid, "video_device", devName)
    continue
default:
    d.logger.Warn("failed to check PID liveness", "pid", ev.Pid, "err", err)
    continue
}
_ = alive
```

---

#### #9 — `reader.Close()` called in both branches of a select; defer would be cleaner (lines ~157–164)

```go
select {
case <-ctx.Done():
    _ = reader.Close()
case err = <-errCh:
    _ = reader.Close()
}
wg.Wait()
```

`reader.Close()` is called in both arms of the select, which is easy to miss when one arm is later added or modified and a `Close` call is forgotten. Because the `ringbuf.Reader` is created locally in `Run` and must always be closed, a deferred close is the idiomatic Go pattern:

```go
reader, err := ringbuf.NewReader(objs.CameraEventRingbuf)
if err != nil {
    return fmt.Errorf("create ringbuf reader: %w", err)
}
defer reader.Close()
```

The goroutine communicating via `errCh` already handles the `ErrClosed` case, so an early close caused by context cancellation is handled correctly.

---

### `v4l2_camera_discoverer.go`

#### #10 — Hardcoded architecture-specific `VIDIOC_QUERYCAP` constant (line ~19)

```go
// This is AI generated and it seems to work.
const VIDIOC_QUERYCAP = 0x80685600
```

The ioctl number is encoded as `_IOR('V', 0, struct v4l2_capability)`. The top bits encode the data direction and the _size of the struct_. On x86-64 the struct is 104 bytes (0x68), giving `0x80685600`. On ARM64 the struct layout should be the same (all fields are fixed-width integers), so the value is likely correct. However:

- The comment admitting this is AI-generated without independent verification erodes confidence.
- The build tag `//go:build linux` is present in the eBPF file and package-level, but there is no architecture constraint (`amd64`) on `v4l2_camera_discoverer.go`. If the package is compiled on an architecture with different struct padding, the constant silently becomes wrong at runtime.

The `TestVIDIOCQuerycapEncodesStructSize` test mitigates this by verifying the constant against the Go struct at test time on the build target. That test should be treated as the authoritative guard and the "AI generated" language should be removed. A comment pointing to the kernel `_IOR` macro definition and the test should replace it.

---

#### #11 — Silently swallowed errors in `V4L2DiscoverDevices` (line ~105)

```go
capability, err := V4L2DeviceCapability(filepath.Base(canonical))
if err != nil {
    continue
}
```

When capability querying fails for a device, the error is silently discarded. This makes debugging difficult when, for example, `/dev/video0` exists but the ioctl fails due to a permission error or an unexpected device type. The caller gets back an empty map with no indication of why any devices were excluded.

**Fix:** Log at `Debug` level when a device is skipped:
```go
if err != nil {
    slog.Debug("skipping device: failed to query capabilities", "device", candidate, "err", err)
    continue
}
```

---

#### #13 — Fragile log-content assertion in `TestScriptAdapter_FailingScript` (line ~183)

```go
logs := buf.String()
if strings.Contains(logs, "output=") {
    t.Errorf("log unexpectedly contains output field: %q", logs)
}
```

This test asserts that a specific key (`output=`) does not appear in the textual log output. This is a negative assertion against the _format_ of the slog text handler, not against the semantic content of the log. It will produce a false failure if `slog`'s text handler changes its formatting, or if any log call anywhere in the path happens to use `output` as a key for a different purpose. A more robust approach would be to implement a custom `slog.Handler` that captures structured log records and assert against the structured fields directly.

---

### `camera_coordinator_test.go`

#### #14 — `defaultTimeout = 50ms` is fragile (line ~43)

```go
const defaultTimeout = 50 * time.Millisecond
```

50 ms is a very short window, especially on a loaded CI machine or a system under memory pressure. Tests that assert "no event arrived within the timeout" will pass spuriously if events arrive slightly after the deadline (masking bugs), and tests that assert "an event arrived within the timeout" may flake.

The value is used across both coordinator and script-adapter tests. Consider either increasing it (e.g. to 500 ms, which is still imperceptible to a developer) or making it a named variable with a comment explaining the tradeoff. The `event_broadcaster_test.go` file independently defines `eventTimeout = 100ms`, creating two different timeout constants for the same test suite — they should be consolidated.

---

### `docs/development/architecture.md`

#### #15 — Stale and inaccurate documentation

The architecture document contains multiple references to components that do not exist in the repository and incorrect API values:

- References "A `DBusAdapter`" and "A `DebugAdapter`" — neither exists.
- References `v4l2_utils.go` — the actual file is `v4l2_camera_discoverer.go`.
- Shows `CameraEventRecordingOn/Off` as `iota` values (i.e. 0 and 1) — the actual values are `1` and `2` (to synchronize with the BPF program, which uses the same constants). A reader following the doc who tries to interpret raw event numbers will get them wrong.
- The `CameraCoordinator` is described as "implements the [CameraDetector] interface" — it does not; it has a separate `Adapter`-based output model.

The document should be updated to reflect the current code structure before it actively misleads new contributors.
