#!/bin/bash

# This script synchronizes the BPF headers from the Linux kernel to this include directory.

set -xe

bpftool btf dump file /sys/kernel/btf/vmlinux format c > vmlinux.h
