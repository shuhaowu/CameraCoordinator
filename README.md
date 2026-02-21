# CameraCoordinator

## Overview

CameraCoordinator is a small Go library and daemon that detects when a webcam
or V4L2 device is turned on or off on Linux and exposes those events so other
tools can perform arbitrary actions (notifications, scripts, ambient lighting
control, etc.). It bundles detectors (for example an eBPF-based vb2 ioctl
detector), notifiers, and example scripts to make integration straightforward.

## Key features

- Detect camera on/off events on Linux (V4L2 / videobuf2 ioctl tracing).
- Pluggable detectors and notifiers (run scripts, print events, broadcast).
- Example udev rules and on/off scripts to integrate with host systems.

## Requirements

- Linux with kernel and distribution that support eBPF and V4L2 devices.
- Go 1.18+ to build from source.

## Build

Clone the repository and build the daemon binary:

```bash
git clone https://github.com/your-org/CameraCoordinator.git
cd CameraCoordinator
go generate ./...
go build -o camera-coordinator ./cmd/camera-coordinator
```

## Run

The `camera-coordinator` binary accepts an optional JSON configuration via
`-config /path/to/file.json`. When no config is provided the program uses a
built-in default: the eBPF `vb2_ioctl` detector and the `print` notifier are
enabled automatically.

Run with an example config included in this repo:

```bash
sudo ./camera-coordinator
```

## Configuration

The configuration is JSON and controls which detectors and notifiers are
enabled. This is passed to the `camera-coordinator -config <file>` command. A
minimal example that enables the eBPF detector and the print notifier looks
like:

```json
{
  "detectors": {
    "ebpf_vb2_ioctl": {
      "enabled": true
    }
  },
  "notifiers": {
    "print": {
      "enabled": true
    },
    "script": {
      "enabled": true,
      "on_script": "./on.sh",
      "off_script": "./off.sh"
    }
  }
}
```

Paths starting with "./" in the `on_script` and `off_script` fields are resolved
relative to the directory containing the JSON configuration file. When invoked
the script is passed two arguments: the event type and the video device.  The
event type argument will be either `recording_on` or `recording_off` (from
`CameraEventType`), and the video device argument is the V4L2 device name, for
example `video0`. So for example, `./on.sh recording_on video0` is a possible
invocation.
