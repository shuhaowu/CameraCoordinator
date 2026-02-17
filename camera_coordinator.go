package cameracoordinator

import "context"

type CameraCoordinator struct {
	detectors []CameraDetector
	events    chan CameraEvent
}

func NewCameraCoordinator(detectors ...CameraDetector) *CameraCoordinator {
	return &CameraCoordinator{
		detectors: detectors,
		events:    make(chan CameraEvent),
	}
}

func (c *CameraCoordinator) Events() <-chan CameraEvent {
	return c.events
}

func (c *CameraCoordinator) Run(context.Context) error {
	return nil
}
