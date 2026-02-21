#!/bin/bash

# This script synchronizes the BPF headers from the Linux kernel to this include directory.
# This is needed and using CO-RE, it can be compile once
# See https://nakryiko.com/posts/bpf-core-reference-guide/

set -xe

# bpftool btf dump file /sys/kernel/btf/vmlinux format c > vmlinux.h
# btf dump videobuf2_v4l2 includes the header content from vmlinux.
bpftool btf dump file /sys/kernel/btf/videobuf2_v4l2 format c > videobuf2_v4l2.h
