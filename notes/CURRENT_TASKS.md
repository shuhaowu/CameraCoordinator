# Current Tasks

## Goal
Implement an MVP `CameraDetector` using `github.com/cilium/ebpf` + eBPF C to detect webcam recording on/off by hooking `vb2_ioctl_streamon` and `vb2_ioctl_streamoff` (no bpftrace runtime dependency).

## Scope (Current Slice)
- Build core domain contracts (`CameraEvent`, `CameraDetector`)
- Add eBPF C probes and Go loader scaffolding
- Stream kernel events into Go and emit normalized `/dev/videoN` events
- Add focused tests for event mapping and detector behavior

## Tasks
- [x] Define core event and detector interfaces
  - [x] Add `CameraEventType`, `CameraEvent`
  - [x] Add `CameraDetector` interface with event channel access and lifecycle
- [x] Implement eBPF probe program
  - [x] Hook `kprobe/vb2_ioctl_streamon`
  - [x] Hook `kprobe/vb2_ioctl_streamoff`
  - [x] Extract filename from `struct file -> f_path.dentry -> d_name.name`
  - [x] Emit compact event struct to userspace via ring buffer
- [x] Implement Go eBPF detector
  - [x] Load BPF objects with `cilium/ebpf`
  - [x] Attach both probes
  - [x] Read ring buffer events and convert to `CameraEvent`
  - [x] Normalize filename to `/dev/videoN`
  - [x] Handle shutdown and cleanup robustly
- [x] Add tests
  - [x] Unit tests for event conversion and filename normalization
  - [x] Unit tests for detector channel semantics and lifecycle
  - [x] Root-gated integration test scaffolding for probe attach path
- [ ] Validate
  - [x] `go test ./...`
  - [ ] Manual runtime verification on Linux webcam stream workload

## Acceptance Criteria
- Detector emits `CameraEventRecordingOn` and `CameraEventRecordingOff` on corresponding kernel calls.
- `VideoFilename` is emitted as `/dev/videoN`.
- No duplicate same-state filtering in detector (dedupe remains coordinator responsibility).
- Probe resources cleanly detach on shutdown.
- Unit tests pass for all added pure-Go logic.

## Deferred
- Coordinator dedupe implementation details
- Camera metadata lookup implementation
- DBus/debug adapters
- Alternative attachment strategies beyond kprobe
