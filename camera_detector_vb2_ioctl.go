package cameracoordinator

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"golang.org/x/sys/unix"
)

// TODO: the target is set to AMD64 only. Figure out a way to parameterize this.
//go:generate go tool bpf2go -tags linux -target amd64 camera_detector_vb2_ioctl bpf/camera_detector_vb2_ioctl.bpf.c -- -I./bpf/include

const ebpfVb2IoctlDetectorName = "EBPFVb2IoctlStreamDetector"

type EBPFVb2IoctlStreamDetector struct {
	events chan CameraEvent
	logger *slog.Logger
}

func NewEBPFVb2IoctlStreamDetector() *EBPFVb2IoctlStreamDetector {
	return &EBPFVb2IoctlStreamDetector{
		events: make(chan CameraEvent),
		logger: slog.With("component", ebpfVb2IoctlDetectorName),
	}
}

func (d *EBPFVb2IoctlStreamDetector) Name() string {
	return ebpfVb2IoctlDetectorName
}

func (d *EBPFVb2IoctlStreamDetector) Events() <-chan CameraEvent {
	return d.events
}

func (d *EBPFVb2IoctlStreamDetector) Run(ctx context.Context) error {
	objs := camera_detector_vb2_ioctlObjects{}
	if err := loadCamera_detector_vb2_ioctlObjects(&objs, nil); err != nil {
		return fmt.Errorf("load bpf objects: %w", err)
	}
	defer objs.Close()

	streamOnLink, err := link.Kprobe("vb2_ioctl_streamon", objs.KprobeVb2IoctlStreamon, nil)
	if err != nil {
		return fmt.Errorf("attach kprobe vb2_ioctl_streamon: %w", err)
	}
	defer streamOnLink.Close()

	streamOffLink, err := link.Kprobe("vb2_ioctl_streamoff", objs.KprobeVb2IoctlStreamoff, nil)
	if err != nil {
		return fmt.Errorf("attach kprobe vb2_ioctl_streamoff: %w", err)
	}
	defer streamOffLink.Close()

	// vb2_fop_release is hooked to detect the case where a process is
	// terminated without calling vb2_ioctl_streamoff. The BPF program only
	// emits a STREAM_OFF event when the device was previously marked as
	// streaming by the streamon kprobe.
	fopReleaseLink, err := link.Kprobe("vb2_fop_release", objs.KprobeVb2FopRelease, nil)
	if err != nil {
		return fmt.Errorf("attach kprobe vb2_fop_release: %w", err)
	}
	defer fopReleaseLink.Close()

	reader, err := ringbuf.NewReader(objs.CameraEventRingbuf)
	if err != nil {
		return fmt.Errorf("create ringbuf reader: %w", err)
	}

	errCh := make(chan error, 1)
	var wg sync.WaitGroup

	// Run the read loop in a goroutine and report its result via errCh. This is
	// needed because reader.ReadInto can block indefinitely, and we need a way to
	// unblock it when the context is cancelled.
	wg.Go(func() {
		var rec ringbuf.Record
		for {
			if err := reader.ReadInto(&rec); err != nil {
				if errors.Is(err, ringbuf.ErrClosed) {
					errCh <- nil
					return
				}
				errCh <- fmt.Errorf("read ringbuf: %w", err)
				return
			}

			var ev camera_detector_vb2_ioctlCameraEvent
			if err := binary.Read(bytes.NewReader(rec.RawSample), binary.NativeEndian, &ev); err != nil {
				errCh <- fmt.Errorf("decode camera event (len=%d): %w", len(rec.RawSample), err)
				return
			}

			// Determine device name and only send events for V4L2 capture devices.
			devName := unix.ByteSliceToString(ev.Name[:])

			// This will run a syscall on every event. It shouldn't be a bottleneck
			// because video on/off should not happen that often.
			cap, err := V4L2DeviceCapability(devName)
			if err != nil {
				d.logger.Warn("failed to query V4L2 capabilities", "err", err, "video_device", devName)
				continue
			}

			if !cap.HasCapabilities(V4L2CapVideoCapture) {
				d.logger.Debug("device is not a camera, ignoring...", "video_device", devName)
				// Not a camera device according to V4L2 capabilities; skip.
				continue
			}

			d.logger.Debug("camera event detected", "video_device", devName, "event_type", ev.EventType, "source", ev.Source)

			select {
			case <-ctx.Done():
				errCh <- nil
				return
			case d.events <- CameraEvent{
				Detector:    ebpfVb2IoctlDetectorName,
				Type:        CameraEventType(ev.EventType),
				VideoDevice: devName,
			}:
			}
		}
	})

	// Wait for either context cancellation or the reader goroutine to finish.
	select {
	case <-ctx.Done():
		// Close the reader to unblock the goroutine if it's blocked in ReadInto.
		_ = reader.Close()
	case err = <-errCh:
		_ = reader.Close()
	}

	wg.Wait()
	close(d.events)
	return err
}

var _ CameraDetector = (*EBPFVb2IoctlStreamDetector)(nil)
