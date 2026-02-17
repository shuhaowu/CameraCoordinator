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

struct camera_event {
    u32 event_type;
    u8 name[32];
};

struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 256 * 1024); // 256 KB
    __type(value, struct camera_event);
} camera_event_ringbuf SEC(".maps");

static __always_inline int emit_event(struct file *file, u32 camera_event_type)
{
    const unsigned char *name_ptr;
    struct camera_event *event;

    if (!file) {
        return 0;
    }

    // Using CORE to get the pointer to the file name.
    name_ptr = BPF_CORE_READ(file, f_path.dentry, d_name.name);
    if (!name_ptr) {
        return 0;
    }

    // If a rervation is made, it seems like we must submit it if not null?
    // TODO: otherwise it seems like we just submit events infinitely.
    event = bpf_ringbuf_reserve(&camera_event_ringbuf, sizeof(*event), 0);
    if (!event) {
        // This can happen if the ring buffer is full. In that case, we just
        // drop the event.
        // TODO: We should emit a counter.
        return 0;
    }

    // Set the event type first
    event->event_type = camera_event_type;

    // Read the filename into the event to be emitted.
    bpf_probe_read_kernel_str(event->name, sizeof(event->name), name_ptr);

    // Submit the event
    bpf_ringbuf_submit(event, 0);

    return 0;
}

SEC("kprobe/vb2_ioctl_streamon")
int kprobe_vb2_ioctl_streamon(struct pt_regs *ctx)
{
    struct file *file = (struct file *)PT_REGS_PARM1(ctx);
    return emit_event(file, CAMERA_EVENT_TYPE_STREAM_ON);
}

SEC("kprobe/vb2_ioctl_streamoff")
int kprobe_vb2_ioctl_streamoff(struct pt_regs *ctx)
{
    struct file *file = (struct file *)PT_REGS_PARM1(ctx);
    return emit_event(file, CAMERA_EVENT_TYPE_STREAM_OFF);
}
