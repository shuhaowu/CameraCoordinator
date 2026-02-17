# Overall Comment

The current codebase is an **early MVP skeleton with one critical correctness bug and one major missing core component**. The strongest part is the narrow eBPF event extraction path and basic mapping tests; however, the repository does not yet satisfy its own architecture contract, and there are reliability gaps that can cause hard hangs in production.

How the reviewed code works today:
- `EBPFVb2IoctlStreamDetector` attaches kprobes to `vb2_ioctl_streamon` / `vb2_ioctl_streamoff`, reads ring-buffer events, maps them into `CameraEvent`, and emits to a Go channel.
- The CLI (`cmd/camera-detector/main.go`) runs the detector and prints events.
- `CameraCoordinator` exists as a type but does not implement any merging/deduping behavior yet.

Top breakage risks:
1. **Potential deadlock/hang in `EBPFVb2IoctlStreamDetector.Run` on non-context read errors** due to defer ordering and goroutine coordination.
2. **`CameraCoordinator.Run` is a stub** and currently violates documented architecture/acceptance expectations.
3. **Test suite is too shallow for lifecycle/error/concurrency behavior**, so regressions in run/cleanup paths can slip through despite green tests.

Overall feeling: **not production-ready yet**. The core idea is sound, but critical lifecycle behavior and contract completeness need work before this is reliable.

# Inline Comments

- **File:** `camera_detector_ebpf_integration_test.go`
  **Function:** `TestEBPFVb2IoctlStreamDetectorAttachIntegration`
  **Line:** 28
  **Issue:** The test skips based on substring matching of error text.
  **Why it matters:** Brittle and can mask real regressions if error messages change or overlap unexpectedly.
  **Fix:** Prefer typed/sentinel error wrapping from production code and assert with `errors.Is` / structured conditions.

- **File:** `camera_detector_ebpf_test.go`
  **Function:** `TestEBPFDetectorEventsChannelInitialized`
  **Lines:** 5-11
  **Issue:** Test only verifies non-nil channel and does not exercise run/cancel/cleanup semantics.
  **Why it matters:** The most failure-prone logic is lifecycle/concurrency; current test coverage does not protect it.
  **Fix:** Add unit/integration tests for: cancellation while blocked on send, ringbuf-close behavior, non-context read error handling (including no deadlock), and channel/runner shutdown expectations.
