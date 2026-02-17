# Overall Comment

The current codebase is an **early MVP skeleton with one critical correctness bug and one major missing core component**. The strongest part is the narrow eBPF event extraction path and basic mapping tests; however, the repository does not yet satisfy its own architecture contract, and there are reliability gaps that can cause hard hangs in production.

How the reviewed code works today:
- `EBPFCameraDetector` attaches kprobes to `vb2_ioctl_streamon` / `vb2_ioctl_streamoff`, reads ring-buffer events, maps them into `CameraEvent`, and emits to a Go channel.
- The CLI (`cmd/camera-detector/main.go`) runs the detector and prints events.
- `CameraCoordinator` exists as a type but does not implement any merging/deduping behavior yet.

Top breakage risks:
1. **Potential deadlock/hang in `EBPFCameraDetector.Run` on non-context read errors** due to defer ordering and goroutine coordination.
2. **`CameraCoordinator.Run` is a stub** and currently violates documented architecture/acceptance expectations.
3. **Test suite is too shallow for lifecycle/error/concurrency behavior**, so regressions in run/cleanup paths can slip through despite green tests.

Overall feeling: **not production-ready yet**. The core idea is sound, but critical lifecycle behavior and contract completeness need work before this is reliable.

# Inline Comments

- **File:** `camera_detector_ebpf_linux.go`  
  **Function:** `(*EBPFCameraDetector).Run`  
  **Line:** 64 (`defer wg.Wait()`), with related lines 55 (`defer reader.Close()`), 67-75 (read loop error return)  
  **Issue:** There is a deadlock path. If `reader.Read()` returns a non-`ErrClosed` error while `ctx` is not canceled, the function returns from line 75, then executes defers in LIFO order. `wg.Wait()` runs before `reader.Close()`, but the goroutine only exits on `<-ctx.Done()`, so `wg.Wait()` can block forever.  
  **Why it matters:** A transient ringbuf read error can hang shutdown and leak resources/process termination.  
  **Fix:** Remove the waiter goroutine entirely and close the reader from a context-aware path that cannot deadlock, or ensure defer order/goroutine exit condition is safe (e.g., goroutine exits on either `ctx.Done()` or a local `done` channel, and `reader.Close()` happens before `wg.Wait()`).

- **File:** `camera_coordinator.go`  
  **Function:** `(*CameraCoordinator).Run`  
  **Lines:** 21-22  
  **Issue:** `Run` is a no-op that immediately returns `nil`.  
  **Why it matters:** This breaks the stated architecture role of coordinator as the merge/dedupe layer and makes the type misleading to callers.  
  **Fix:** Implement actual lifecycle behavior: start child detectors, fan-in events, apply per-device same-state dedupe, propagate cancellation/errors, and close output channel on shutdown.

- **File:** `camera_coordinator.go`  
  **Function:** `NewCameraCoordinator` / `Events`  
  **Lines:** 8-18  
  **Issue:** The struct exposes an events channel that is never written to by any logic.  
  **Why it matters:** API advertises behavior that does not exist; consumers can block forever waiting on events from coordinator.  
  **Fix:** Either fully wire event fan-in or avoid exposing coordinator as runnable detector until implementation exists.

- **File:** `docs/development/architecture.md`  
  **Section:** Coordinator description  
  **Line:** 7  
  **Issue:** Documentation claims coordinator merge + dedupe behavior that code does not implement.  
  **Why it matters:** This creates a dangerous trust gap; maintainers/users will assume behavior that is absent.  
  **Fix:** Either implement the documented behavior now or clearly mark coordinator functionality as planned/deferred in architecture docs.

- **File:** `notes/CURRENT_TASKS.md`  
  **Section:** Acceptance Criteria  
  **Line:** 38  
  **Issue:** Notes state dedupe is coordinator responsibility, but coordinator is currently non-functional.  
  **Why it matters:** End-to-end semantics are incomplete; duplicate state events can flow to consumers with no current mitigation path.  
  **Fix:** Add an explicit task/blocker item for coordinator implementation and reflect current behavior limits in acceptance status.

- **File:** `camera_detector_ebpf_linux.go`  
  **Function:** `(*EBPFCameraDetector).Run`  
  **Lines:** 79-80  
  **Issue:** Binary decode failures are silently dropped (`continue` with no accounting/logging).  
  **Why it matters:** Silent data-path corruption is hard to diagnose in production, especially under kernel/program mismatch.  
  **Fix:** Track and report decode failures (counter/log hook/returned wrapped error after threshold) so operators can detect malformed event streams.

- **File:** `camera_detector_ebpf_linux.go`  
  **Function:** `(*EBPFCameraDetector).Run`  
  **Line:** 79 (`binary.Read(bytes.NewReader(...))`)  
  **Issue:** Per-event decoding allocates/uses reflection-heavy path (`bytes.NewReader` + `binary.Read`) in the hot loop.  
  **Why it matters:** This is avoidable overhead in a potentially high-frequency path; it adds GC pressure and latency.  
  **Fix:** Decode using fixed-size checks plus direct copy/unmarshal without `binary.Read` reflection (e.g., manual field extraction or `unsafe` with strict size guards).

- **File:** `generate_ebpf_linux.go`  
  **Line:** 3  
  **Issue:** `go:generate` hardcodes a versioned header include path under `$GOPATH/pkg/mod/...@v0.20.0/...`.  
  **Why it matters:** Fragile/non-reproducible across machines, module cache layouts, and version bumps; this will frequently break regeneration.  
  **Fix:** Use a stable include strategy (checked-in headers, `clang` include paths from project-local directory, or derive module path dynamically in script).

- **File:** `camera_detector_ebpf_integration_test.go`  
  **Function:** `TestEBPFCameraDetectorAttachIntegration`  
  **Line:** 28  
  **Issue:** The test skips based on substring matching of error text.  
  **Why it matters:** Brittle and can mask real regressions if error messages change or overlap unexpectedly.  
  **Fix:** Prefer typed/sentinel error wrapping from production code and assert with `errors.Is` / structured conditions.

- **File:** `camera_detector_ebpf_linux_test.go`  
  **Function:** `TestEBPFDetectorEventsChannelInitialized`  
  **Lines:** 5-11  
  **Issue:** Test only verifies non-nil channel and does not exercise run/cancel/cleanup semantics.  
  **Why it matters:** The most failure-prone logic is lifecycle/concurrency; current test coverage does not protect it.  
  **Fix:** Add unit/integration tests for: cancellation while blocked on send, ringbuf-close behavior, non-context read error handling (including no deadlock), and channel/runner shutdown expectations.

- **File:** `bpf/camera_detector.bpf.c`  
  **Function:** `emit_event`  
  **Lines:** 48-78  
  **Issue:** Event emission has no filtering/validation beyond dentry name presence; any streamon/off through this path is surfaced as camera activity.  
  **Why it matters:** Can create false positives if non-camera V4L2/video nodes trigger these functions in some environments.  
  **Fix:** Add stricter filtering criteria (where feasible in-kernel or userspace), and document known false-positive boundaries explicitly.

- **File:** `cmd/camera-detector/main.go`  
  **Function:** `main`  
  **Lines:** 28-31  
  **Issue:** On `ctx.Done()`, the code blocks waiting for `<-errCh`; if detector teardown hangs, CLI shutdown also hangs.  
  **Why it matters:** CLI responsiveness and clean exit depend on detector internals not stalling.  
  **Fix:** Use bounded wait (timeout/select) or ensure detector `Run` cannot deadlock; surface timeout diagnostics on forced shutdown.

- **File:** `camera_detector_ebpf.go`  
  **Function:** `normalizeVideoFilename`  
  **Lines:** 29-39  
  **Issue:** Non-empty non-`/dev/` input is blindly prefixed with `/dev/`, even if already absolute or malformed (e.g., `/tmp/x` -> `/dev//tmp/x`).  
  **Why it matters:** Assumptions are currently tied to dentry basename; if event source changes or malformed data appears, path output becomes invalid but looks normalized.  
  **Fix:** Enforce stricter expected pattern (`video[0-9]+`) or only normalize known-safe basenames; reject unexpected paths.
