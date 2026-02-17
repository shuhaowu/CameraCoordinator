//go:build !ebpf

package cameracoordinator

import "context"

type EBPFCameraDetector struct {
	events chan CameraEvent
}

func NewEBPFCameraDetector() *EBPFCameraDetector {
	return &EBPFCameraDetector{events: make(chan CameraEvent)}
}

func (d *EBPFCameraDetector) Events() <-chan CameraEvent {
	return d.events
}

func (d *EBPFCameraDetector) Run(context.Context) error {
	return ErrEBPFUnavailable
}
