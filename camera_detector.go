package cameracoordinator

import "context"

type CameraDetector interface {
	Events() <-chan CameraEvent
	Run(context.Context) error
	Name() string
}
