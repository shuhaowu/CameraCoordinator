package cameracoordinator

import "context"

type CameraDetector interface {
	Run(context.Context, chan<- CameraEvent) error
	Name() string
}
