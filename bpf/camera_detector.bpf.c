#include "common.h"
#include "bpf_tracing.h"

struct qstr {
    union {
        struct {
            __u32 hash;
            __u32 len;
        };
        __u64 hash_len;
    };
    const unsigned char *name;
} __attribute__((preserve_access_index));

struct dentry {
    struct qstr d_name;
} __attribute__((preserve_access_index));

struct vfsmount;

struct path {
    struct vfsmount *mnt;
    struct dentry *dentry;
} __attribute__((preserve_access_index));

struct file {
    struct path f_path;
} __attribute__((preserve_access_index));

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
    struct dentry *dentry;
    const unsigned char *name_ptr;
    struct camera_event *event;

    if (!file) {
        return 0;
    }

    if (bpf_probe_read_kernel(&dentry, sizeof(dentry), &file->f_path.dentry) != 0) {
        return 0;
    }
    if (!dentry) {
        return 0;
    }

    if (bpf_probe_read_kernel(&name_ptr, sizeof(name_ptr), &dentry->d_name.name) != 0) {
        return 0;
    }
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
