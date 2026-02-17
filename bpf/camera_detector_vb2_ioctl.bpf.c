//go:build ignore

#include "vmlinux.h"

#include <bpf/bpf_core_read.h>
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>

char LICENSE[] SEC("license") = "GPL";

enum event_type {
    EVENT_TYPE_STREAM_ON = 1,
    EVENT_TYPE_STREAM_OFF = 2,
};

struct camera_event {
    __u8 event_type;
    __u8 _pad[3];
    char name[16];
};

struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 1 << 20);
} camera_events SEC(".maps");

static __always_inline int emit_event(struct file *file, __u8 event_type)
{
    const unsigned char *name_ptr;
    struct camera_event *event;

    if (!file) {
        return 0;
    }

    name_ptr = BPF_CORE_READ(file, f_path.dentry, d_name.name);
    if (!name_ptr) {
        return 0;
    }

    event = bpf_ringbuf_reserve(&camera_events, sizeof(*event), 0);
    if (!event) {
        return 0;
    }

    event->event_type = event_type;
    bpf_probe_read_kernel_str(event->name, sizeof(event->name), name_ptr);
    bpf_ringbuf_submit(event, 0);

    return 0;
}

SEC("kprobe/vb2_ioctl_streamon")
int kprobe_vb2_ioctl_streamon(struct pt_regs *ctx)
{
    struct file *file = (struct file *)PT_REGS_PARM1(ctx);
    return emit_event(file, EVENT_TYPE_STREAM_ON);
}

SEC("kprobe/vb2_ioctl_streamoff")
int kprobe_vb2_ioctl_streamoff(struct pt_regs *ctx)
{
    struct file *file = (struct file *)PT_REGS_PARM1(ctx);
    return emit_event(file, EVENT_TYPE_STREAM_OFF);
}
