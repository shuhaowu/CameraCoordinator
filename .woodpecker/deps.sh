#!/bin/bash

set -xe

export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y --no-install-recommends \
  clang \
  llvm \
  bpftool \
  libbpf-dev \
  libudev-dev
