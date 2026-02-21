//go:build ignore

#include "vmlinux.h"

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
  u8 name[32];
};

struct {
  __uint(type, BPF_MAP_TYPE_RINGBUF);
  __uint(max_entries, 256 * 1024); // 256 KB
  __type(value, struct camera_event);
} camera_event_ringbuf SEC(".maps");

// Tracks which devices are currently streaming (set by streamon, cleared by
// streamoff or vb2_fop_release). The key is the 32-byte device name.
struct device_key {
  u8 name[32];
};

struct {
  __uint(type, BPF_MAP_TYPE_HASH);
  __uint(max_entries, 64);
  __type(key, struct device_key);
  __type(value, u32);
} streaming_state SEC(".maps");

// Emit a camera event for the device identified by name_ptr.
// Callers are responsible for ensuring name_ptr is non-NULL before calling.
static __always_inline int emit_event(const unsigned char *name_ptr, u32 camera_event_type, u32 source)
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
    return 0;
  }

  // Set the event type first
  event->event_type = camera_event_type;
  event->source = source;

  // Read the filename into the event to be emitted.
  bpf_probe_read_kernel_str(event->name, sizeof(event->name), name_ptr);

  // Submit the event
  bpf_ringbuf_submit(event, 0);

  return 0;
}

// Update the streaming state map for the device identified by name_ptr.
// stream_on=1 marks the device as streaming; stream_on=0 clears the entry.
// Callers are responsible for ensuring name_ptr is non-NULL before calling.
static __always_inline void mark_stream_state(const unsigned char *name_ptr, u32 stream_on)
{
  struct device_key key = {};
  bpf_probe_read_kernel_str(key.name, sizeof(key.name), name_ptr);

  if (stream_on) {
    bpf_map_update_elem(&streaming_state, &key, &stream_on, BPF_ANY);
  } else {
    bpf_map_delete_elem(&streaming_state, &key);
  }
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

  // Mark this device as streaming so vb2_fop_release knows to emit an event
  // if the process is terminated without calling streamoff.
  mark_stream_state(name_ptr, 1);
  return emit_event(name_ptr, CAMERA_EVENT_TYPE_STREAM_ON, CAMERA_EVENT_SOURCE_IOCTL);
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

  // Clear the streaming flag for this device.
  mark_stream_state(name_ptr, 0);
  return emit_event(name_ptr, CAMERA_EVENT_TYPE_STREAM_OFF, CAMERA_EVENT_SOURCE_IOCTL);
}

// In some cases the app does not call vb2_ioctl_streamoff before the process
// is terminated. As a part of that termination, this code path is called.
// However this function is called for other reasons too, so we only emit a
// STREAM_OFF event if the device was previously marked as streaming via
// vb2_ioctl_streamon.
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

  struct device_key key = {};
  bpf_probe_read_kernel_str(key.name, sizeof(key.name), name_ptr);

  u32 *streaming = bpf_map_lookup_elem(&streaming_state, &key);
  if (!streaming || *streaming != 1) {
    return 0;
  }

  // The device was streaming and was released without an explicit streamoff.
  mark_stream_state(name_ptr, 0);
  return emit_event(name_ptr, CAMERA_EVENT_TYPE_STREAM_OFF, CAMERA_EVENT_SOURCE_FOP_RELEASE);
}
