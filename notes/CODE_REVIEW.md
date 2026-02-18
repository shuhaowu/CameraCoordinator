# Code Review

## Overall Comment

The codebase is small, focused, and generally well-structured for what it does: use an eBPF kprobe to intercept Linux V4L2 stream-on/off calls, filter for capture devices, and produce deduplicated on/off events to callers. The architectural layering (BPF program → `EBPFVb2IoctlStreamDetector` → `CameraCoordinator`) is clean.

**How the code works:**

1. `bpf/camera_detector_vb2_ioctl.bpf.c` attaches kprobes to `vb2_ioctl_streamon` and `vb2_ioctl_streamoff` in the kernel. When either fires it reads the file's `d_name.name` (the device node basename, e.g. `video0`) and writes a `camera_event` struct into a BPF ring buffer.
2. `EBPFVb2IoctlStreamDetector` (in `camera_detector_vb2_ioctl.go`) loads the BPF objects, attaches the kprobes, then reads from the ring buffer in a goroutine. For each raw event it queries the V4L2 capability of the device via `VIDIOC_QUERYCAP` and drops non-capture devices. Accepted events are forwarded onto the `events` channel.
3. `CameraCoordinator` (in `camera_coordinator.go`) fans events from N detectors into a single internal channel, then an event-handler goroutine tracks per-device reference counts: first `ON` is forwarded, last `OFF` is forwarded; intermediate events and stray `OFF`s are suppressed.
4. `v4l2_camera_discoverer.go` provides a utility to enumerate `/dev/video*` nodes and query their V4L2 capabilities, used by `main.go` for startup logging and by the detector for per-event capability checks.

**Top issues that can cause breakages or correctness problems:**

## Inline Comments


### `v4l2_camera_discoverer.go`

**#14** — `V4L2DeviceCapability` **`unix.Syscall` instead of `unix.IoctlSetInt` or a typed ioctl helper**

```go
_, _, errno := unix.Syscall(unix.SYS_IOCTL, f.Fd(), uintptr(VIDIOC_QUERYCAP), uintptr(unsafe.Pointer(&cap)))
```

Using raw `unix.Syscall` with `unsafe.Pointer` bypasses the `x/sys/unix` typed ioctl helpers and is harder to audit for correctness. `unix.IoctlRetInt` or a custom `unix.Syscall6` is acceptable for this case, but the code should at minimum explain why the higher-level helpers cannot be used (if there is such a reason). Additionally, `VIDIOC_QUERYCAP` is manually defined as `0x80685600` — if this constant ever diverges from the kernel's value due to struct layout changes, the ioctl will silently fail. Consider using the `x/sys/unix` constant if available or asserting the value with a compile-time check.
