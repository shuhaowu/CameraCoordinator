package cameracoordinator

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"

	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"golang.org/x/sys/unix"
)

// TODO: the target is set to AMD64 only. Figure out a way to parameterize this.
//go:generate go tool bpf2go -tags linux -target amd64 camera_detector_vb2_ioctl bpf/camera_detector_vb2_ioctl.bpf.c -- -I./bpf/include

type EBPFVb2IoctlStreamDetector struct {
	events chan CameraEvent
}

func NewEBPFVb2IoctlStreamDetector() *EBPFVb2IoctlStreamDetector {
	return &EBPFVb2IoctlStreamDetector{events: make(chan CameraEvent)}
}

func (d *EBPFVb2IoctlStreamDetector) Name() string {
	return "ebpf/vb2_ioctl_stream{on,off}"
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

	reader, err := ringbuf.NewReader(objs.CameraEventRingbuf)
	if err != nil {
		return fmt.Errorf("create ringbuf reader: %w", err)
	}
	defer reader.Close()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-ctx.Done()
		_ = reader.Close()
	}()
	defer wg.Wait()

	var rec ringbuf.Record
	for {
		if err := reader.ReadInto(&rec); err != nil {
			if errors.Is(err, ringbuf.ErrClosed) {
				return nil
			}
			if errors.Is(ctx.Err(), context.Canceled) {
				return nil
			}
			return fmt.Errorf("read ringbuf: %w", err)
		}

		var ev camera_detector_vb2_ioctlCameraEvent
		// binary.Read is a reflection based API and can be made more efficient if
		// we implement decoder manually with. However this is not worth it for now
		// as this event is not frequent.
		if err := binary.Read(bytes.NewReader(rec.RawSample), binary.LittleEndian, &ev); err != nil {
			return fmt.Errorf("decode camera event (len=%d): %w", len(rec.RawSample), err)
		}

		select {
		case <-ctx.Done():
			return nil
		case d.events <- CameraEvent{
			Type:          CameraEventType(ev.EventType),
			VideoFilename: unix.ByteSliceToString(ev.Name[:]),
		}:
		}
	}
}

var _ CameraDetector = (*EBPFVb2IoctlStreamDetector)(nil)
