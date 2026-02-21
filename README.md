CameraCoordinator
=================

Build instructions
------------------

- Git clone recursive

Configuration
-------------

The `camera-coordinator` binary accepts an optional JSON configuration via
`-config /path/to/file.json`.  When no config is provided the program uses a built-in default: the
EBPF `vb2_ioctl` detector and the `print` adapter are enabled automatically.
This ensures the binary is useful out of the box while still allowing an
override via `-config`.

A simple configuration enabling only the print adapter looks like:

```json
{
  "detectors": {
    "ebpf_vb2_ioctl": {}
  },
  "adapters": {
    "print": {}
  }
}
```

Each component object may include an `"enabled"` boolean (omitted ⇒ true).
There is no per-component `"config"` field today, simplifying the schema and
avoiding unused JSON keys.  The struct can be extended later if specific
components require configuration.
