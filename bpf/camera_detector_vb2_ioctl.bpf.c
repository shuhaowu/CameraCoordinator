//go:build ignore

// camera_detector_vb2_ioctl.bpf.c
//
// Detects when a V4L2 camera device starts and stops streaming by attaching
// kprobes to three kernel functions. Events are delivered to userspace via a
// BPF ring buffer.
//
// --- Probes ---
//
// kprobe/vb2_ioctl_streamon
//   Fires when a userspace process calls the VIDIOC_STREAMON ioctl on a V4L2
//   device. Emits a CAMERA_EVENT_TYPE_STREAM_ON event and records the TGID
//   (userspace PID) of the calling process in the streaming_pid map, keyed by
//   device name (e.g. "video0").
//
// kprobe/vb2_ioctl_streamoff
//   Fires when a userspace process calls the VIDIOC_STREAMOFF ioctl. Emits a
//   CAMERA_EVENT_TYPE_STREAM_OFF event and removes the device's entry from
//   streaming_pid so that a subsequent vb2_fop_release for the same device
//   does not emit a duplicate STREAM_OFF.
//
// kprobe/vb2_fop_release
//   Fires whenever any process closes a V4L2 file descriptor. This covers the
//   case where a process that started streaming is terminated (e.g. killed or
//   crashed) without first calling VIDIOC_STREAMOFF.
//
//   Caveat: vb2_fop_release fires for every close of a V4L2 file descriptor,
//   not only when the streaming process exits. For example, any process that
//   opens /dev/videoX to probe its capabilities (VIDIOC_QUERYCAP) and then
//   closes it will also trigger this probe (which the Go side code in this
//   project will do after receiving an event, which could potentially trigger
//   an infinite recursion). To avoid emitting spurious STREAM_OFF events from
//   such processes, the probe looks up streaming_pid for the device and only
//   emits an event if the current TGID matches the one that originally called
//   vb2_ioctl_streamon. The map entry is deleted after the event is emitted so
//   no further fop_release calls for the same device can fire. On the consumer
//   side, they should check if that PID is still alive (or possibly have
//   changed comm which indicates it's a different process).
//
// --- State: streaming_pid map ---
//
//   BPF_MAP_TYPE_HASH keyed by device name → u32 TGID.
//   Written by: kprobe_vb2_ioctl_streamon  (BPF_ANY update).
//   Deleted by: kprobe_vb2_ioctl_streamoff (clean shutdown path).
//               kprobe_vb2_fop_release     (crash/kill path, on PID match).
//
//   If two processes stream from the same device concurrently (unusual but
//   possible), the second streamon overwrites the first PID. This means the
//   fop_release guard will only recognise the second process, and a release
//   from the first process will be silently dropped. This is an acceptable
//   trade-off given that concurrent streaming from the same device node is
//   not a typical use case for a webcam.

#include "videobuf2_v4l2.h"

#include <bpf/bpf_core_read.h>
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>

char LICENSE[] SEC("license") = "GPL";

// Synchronize this with CameraEventType in camera_event.go
enum camera_event_type {
  CAMERA_EVENT_TYPE_STREAM_ON = 1,
  CAMERA_EVENT_TYPE_STREAM_OFF = 2,
};

enum camera_event_source {
  CAMERA_EVENT_SOURCE_IOCTL = 1,
  CAMERA_EVENT_SOURCE_FOP_RELEASE = 2,
};

struct camera_event {
  u32 event_type;
  u32 source;
  u32 pid;
  u8 name[32];
};

struct {
  __uint(type, BPF_MAP_TYPE_RINGBUF);
  __uint(max_entries, 256 * 1024); // 256 KB
  __type(value, struct camera_event);
} camera_event_ringbuf SEC(".maps");

// Key for streaming_pid map: the device name (e.g. "video0"), matching the
// name field in struct camera_event.
struct device_name_key {
  u8 name[32];
};

// Maps device name → PID (TGID) of the process that called
// vb2_ioctl_streamon. Used by kprobe_vb2_fop_release to filter out release
// calls from other processes (e.g. processes probing device capabilities),
// which would otherwise generate spurious STREAM_OFF events.
struct {
  __uint(type, BPF_MAP_TYPE_HASH);
  __uint(max_entries, 64);
  __type(key, struct device_name_key);
  __type(value, u32);
} streaming_pid SEC(".maps");

// Emit a camera event for the device identified by name_ptr.
// Callers are responsible for ensuring name_ptr is non-NULL before calling.
static __always_inline void emit_event(const unsigned char *name_ptr, u32 camera_event_type, u32 source, u32 pid)
{
  struct camera_event *event;

  // We should reserve only after we make sure we want to send an event,
  // because reserving space will cause it to send (unless cancelled)? That's
  // what it seems like it happens during testing.
  event = bpf_ringbuf_reserve(&camera_event_ringbuf, sizeof(*event), 0);
  if (!event) {
    // This can happen if the ring buffer is full. In that case, we just
    // drop the event.
    // TODO: We should emit a counter.
    return;
  }

  // Set the event type first
  event->event_type = camera_event_type;
  event->source = source;
  event->pid = pid;

  // Read the filename into the event to be emitted.
  bpf_probe_read_kernel_str(event->name, sizeof(event->name), name_ptr);

  // Submit the event
  bpf_ringbuf_submit(event, 0);
}

// Store the current task's PID (TGID) under the supplied device name.
static __always_inline void record_streaming_pid(const unsigned char *name_ptr, u32 pid)
{
  struct device_name_key key = {};
  bpf_probe_read_kernel_str(key.name, sizeof(key.name), name_ptr);
  bpf_map_update_elem(&streaming_pid, &key, &pid, BPF_ANY);
}

// Remove an existing entry for the supplied device name.
static __always_inline void clear_streaming_pid(const unsigned char *name_ptr)
{
  struct device_name_key key = {};
  bpf_probe_read_kernel_str(key.name, sizeof(key.name), name_ptr);
  bpf_map_delete_elem(&streaming_pid, &key);
}

// Look up the stored PID for the device; if it matches the current task
// remove the entry and return 1.  Otherwise return 0.
static __always_inline int check_streaming_pid_and_clear(const unsigned char *name_ptr, u32 pid)
{
  struct device_name_key key = {};
  bpf_probe_read_kernel_str(key.name, sizeof(key.name), name_ptr);

  u32 *stored_pid = bpf_map_lookup_elem(&streaming_pid, &key);
  if (!stored_pid) {
    return 0;
  }

  if (*stored_pid != pid) {
    return 0;
  }

  bpf_map_delete_elem(&streaming_pid, &key);
  return 1;
}

SEC("kprobe/vb2_ioctl_streamon")
int kprobe_vb2_ioctl_streamon(struct pt_regs *ctx)
{
  struct file *file = (struct file *)PT_REGS_PARM1(ctx);
  if (!file) {
    return 0;
  }

  const unsigned char *name_ptr = BPF_CORE_READ(file, f_path.dentry, d_name.name);
  if (!name_ptr) {
    return 0;
  }

  const u32 pid = bpf_get_current_pid_tgid() >> 32;

  emit_event(name_ptr, CAMERA_EVENT_TYPE_STREAM_ON, CAMERA_EVENT_SOURCE_IOCTL, pid);
  record_streaming_pid(name_ptr, pid);

  return 0;
}

SEC("kprobe/vb2_ioctl_streamoff")
int kprobe_vb2_ioctl_streamoff(struct pt_regs *ctx)
{
  struct file *file = (struct file *)PT_REGS_PARM1(ctx);
  if (!file) {
    return 0;
  }

  const unsigned char *name_ptr = BPF_CORE_READ(file, f_path.dentry, d_name.name);
  if (!name_ptr) {
    return 0;
  }

  const u32 pid = bpf_get_current_pid_tgid() >> 32;

  emit_event(name_ptr, CAMERA_EVENT_TYPE_STREAM_OFF, CAMERA_EVENT_SOURCE_IOCTL, pid);
  clear_streaming_pid(name_ptr);

  return 0;
}

// In some cases the app does not call vb2_ioctl_streamoff before the process
// is terminated. As a part of that termination, this code path is called.
// However this function is called for many other reasons too (e.g. any process
// closing a V4L2 file descriptor). We guard against false events by only
// emitting STREAM_OFF when the release comes from the same PID (TGID) that
// originally called vb2_ioctl_streamon for this device.
SEC("kprobe/vb2_fop_release")
int kprobe_vb2_fop_release(struct pt_regs *ctx)
{
  struct file *file = (struct file *)PT_REGS_PARM1(ctx);
  if (!file) {
    return 0;
  }

  const unsigned char *name_ptr = BPF_CORE_READ(file, f_path.dentry, d_name.name);
  if (!name_ptr) {
    return 0;
  }

  const u32 pid = bpf_get_current_pid_tgid() >> 32;

  /* Look up and clear the PID entry if it matches the current task.  The
   * helper returns non-zero when a match was found so we should emit an
   * event. */
  if (!check_streaming_pid_and_clear(name_ptr, pid)) {
    return 0;
  }

  emit_event(name_ptr, CAMERA_EVENT_TYPE_STREAM_OFF, CAMERA_EVENT_SOURCE_FOP_RELEASE, pid);
  return 0;
}
