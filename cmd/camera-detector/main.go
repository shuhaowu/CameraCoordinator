package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os/signal"
	"syscall"
	"time"

	cameracoordinator "github.com/shuhaowu/CameraCoordinator"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	detector := cameracoordinator.NewEBPFCameraDetector()
	errCh := make(chan error, 1)

	go func() {
		errCh <- detector.Run(ctx)
	}()

	for {
		select {
		case <-ctx.Done():
			err := <-errCh
			if err != nil && !errors.Is(err, context.Canceled) {
				log.Fatalf("detector stopped with error: %v", err)
			}
			return
		case err := <-errCh:
			if err != nil && !errors.Is(err, context.Canceled) {
				log.Fatalf("detector stopped with error: %v", err)
			}
			return
		case event := <-detector.Events():
			fmt.Printf("%s event=%s device=%s\n", time.Now().Format(time.RFC3339Nano), eventTypeString(event.Type), event.VideoFilename)
		}
	}
}

func eventTypeString(eventType cameracoordinator.CameraEventType) string {
	switch eventType {
	case cameracoordinator.CameraEventRecordingOn:
		return "recording_on"
	case cameracoordinator.CameraEventRecordingOff:
		return "recording_off"
	default:
		return "unknown"
	}
}
