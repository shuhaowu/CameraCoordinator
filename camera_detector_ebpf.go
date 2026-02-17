package cameracoordinator

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
)

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -no-strip -target amd64 -cc clang -cflags "-O2 -g -I$GOPATH/pkg/mod/github.com/cilium/ebpf@v0.20.0/examples/headers" cameraDetector bpf/camera_detector.bpf.c

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

type ebpfRawEvent struct {
	EventType uint8
	_         [3]byte
	Name      [16]byte
}

func mapRawEventToCameraEvent(event ebpfRawEvent) (CameraEvent, bool) {
	name := normalizeVideoFilename(cStringToGo(event.Name[:]))
	if name == "" {
		return CameraEvent{}, false
	}

	switch event.EventType {
	case 1:
		return CameraEvent{Type: CameraEventRecordingOn, VideoFilename: name}, true
	case 2:
		return CameraEvent{Type: CameraEventRecordingOff, VideoFilename: name}, true
	default:
		return CameraEvent{}, false
	}
}

func normalizeVideoFilename(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return ""
	}

	if strings.HasPrefix(trimmed, "/dev/") {
		return trimmed
	}

	return "/dev/" + trimmed
}

func cStringToGo(raw []byte) string {
	for index := range raw {
		if raw[index] == 0 {
			return string(raw[:index])
		}
	}

	return string(raw)
}

var _ CameraDetector = (*EBPFVb2IoctlStreamDetector)(nil)
