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
//go:generate go tool bpf2go -verbose -tags linux -target amd64 camera_detector_vb2_ioctl bpf/camera_detector_vb2_ioctl.bpf.c -- -I./bpf/include

const ebpfVb2IoctlDetectorName = "EBPFVb2IoctlStreamDetector"

type EBPFVb2IoctlStreamDetector struct {
	logger *slog.Logger
}

func NewEBPFVb2IoctlStreamDetector() *EBPFVb2IoctlStreamDetector {
	return &EBPFVb2IoctlStreamDetector{
		logger: slog.With("component", ebpfVb2IoctlDetectorName),
	}
}

func (d *EBPFVb2IoctlStreamDetector) Name() string {
	return ebpfVb2IoctlDetectorName
}

func (d *EBPFVb2IoctlStreamDetector) Run(ctx context.Context, ch chan<- CameraEvent) error {
	objs := camera_detector_vb2_ioctlObjects{}
	if err := loadCamera_detector_vb2_ioctlObjects(&objs, nil); err != nil {
		// To debug verifier errors, uncomment this.
		// e := errors.Unwrap(errors.Unwrap(err)).(*ebpf.VerifierError)
		// for _, l := range e.Log {
		// 	fmt.Println(l)
		// }

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
	// emits a STREAM_OFF event when the kernel's vb2_queue reports the device
	// is currently streaming (checked via file->private_data->vdev->queue->streaming).
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

			// Source 2 means the event is from vb2_fop_release, which can be
			// triggered by process termination.
			//
			// TODO: this doesn't quite work. Sometimes vb2_fop_release is called and
			// the process doesn't die for another 500ms. This makes this check be
			// subject to a lot of false positive.
			if ev.Source == 2 {
				err := unix.Kill(int(ev.Pid), 0)
				// events from vb2_fop_release are generated when a fd is closed,
				// often as part of process termination. Otherwise, the process might be
				// doing something else with that fd (like sending some IOCTL to it).
				if err == nil {
					d.logger.Debug("fop_release event PID still alive", "pid", ev.Pid, "video_device", devName)
					continue
				} else if err == unix.ESRCH {
					// This is fine
				} else {
					d.logger.Warn("failed to check pid liveness", "pid", ev.Pid, "err", err)
					continue
				}
			}

			d.logger.Debug("camera event detected", "video_device", devName, "event_type", ev.EventType, "source", ev.Source, "pid", ev.Pid)

			select {
			case <-ctx.Done():
				errCh <- nil
				return
			case ch <- CameraEvent{
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
	return err
}

var _ CameraDetector = (*EBPFVb2IoctlStreamDetector)(nil)
