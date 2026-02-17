//go:build linux && ebpf

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
	"github.com/cilium/ebpf/rlimit"
)

type EBPFCameraDetector struct {
	events chan CameraEvent
}

func NewEBPFCameraDetector() *EBPFCameraDetector {
	return &EBPFCameraDetector{events: make(chan CameraEvent)}
}

func (d *EBPFCameraDetector) Events() <-chan CameraEvent {
	return d.events
}

func (d *EBPFCameraDetector) Run(ctx context.Context) error {
	if err := rlimit.RemoveMemlock(); err != nil {
		return fmt.Errorf("remove memlock rlimit: %w", err)
	}

	objs := cameraDetectorObjects{}
	if err := loadCameraDetectorObjects(&objs, nil); err != nil {
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

	reader, err := ringbuf.NewReader(objs.CameraEvents)
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

	for {
		record, err := reader.Read()
		if err != nil {
			if errors.Is(err, ringbuf.ErrClosed) {
				return nil
			}
			if errors.Is(ctx.Err(), context.Canceled) {
				return nil
			}
			return fmt.Errorf("read ringbuf: %w", err)
		}

		var raw ebpfRawEvent
		if err := binary.Read(bytes.NewReader(record.RawSample), binary.LittleEndian, &raw); err != nil {
			continue
		}

		event, ok := mapRawEventToCameraEvent(raw)
		if !ok {
			continue
		}

		select {
		case <-ctx.Done():
			return nil
		case d.events <- event:
		}
	}
}

var _ CameraDetector = (*EBPFCameraDetector)(nil)
