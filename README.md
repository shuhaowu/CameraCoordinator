# CameraCoordinator

## Overview

CameraCoordinator is a Golang based library and daemon that detects when a
webcam is turned on or off on Linux. On these events, a DBus event is triggered
and scripts can be optionally executed. This allows for things such as:

- Automatically controlling USB-based stream lights such as the Logitech Litra
  lights via the [`autolight`](./autolight/) program.
- Sending a notification that the camera has been turned on to the desktop via
  a [custom script](./examples/notification).

## How it works under the hood

By default, we use eBPF to trace specific kernel functions
(`vb2_ioctl_streamon`/`vb2_ioctl_streamoff` and others) to determine when
cameras are turned on or off. Upon detection of camera on/off, a signal is then
sent via dbus's system bus and/or a script is executed according to
configuration.

## Run

CameraCoordinator is a root daemon as it needs to insert a BPF program into
the kernel.

Basic usage:

```bash
sudo ./camera-coordinator
```

Run a script when the camera is detected

```bash
sudo ./camera-coordinator -on-script path-to-on-script.sh -off-script path-to-off-script.sh
```

Notes:

- Running as root (sudo) is required for attaching the eBPF program.

## Build

Clone the repository and build the daemon binary:

```bash
git clone https://github.com/your-org/CameraCoordinator.git
cd CameraCoordinator
go generate ./... # Optional. Can try without it first.
go build -o camera-coordinator ./cmd/camera-coordinator
```

The `go generate ./...` step will compile the BPF code. This requires `clang`
and `llvm` to be installed on your system.

# Autolight

[Autolight](./autolight/) is an user daemon that's bundled in this repo that
listens to the DBus signal for camera on/off events and will turn on/off
[Logitech Litra](https://www.logitech.com/en-ca/shop/c/cameras-lighting) lights.

Running Autolight together with CameraCoordinator as two background services
will allow the lights to turn automatically on and off based on the state of the
webcam:

![Logitech Litra Glow automatically turning on and off when web cam turns on and off on Linux](demo.avif)
