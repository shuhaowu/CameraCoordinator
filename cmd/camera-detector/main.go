package main

import (
	"context"
	"log/slog"
	"os/signal"
	"sort"
	"sync"
	"syscall"
	"time"

	"github.com/shuhaowu/cameracoordinator"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	discovery := &cameracoordinator.CameraDiscoveryV4L2{}
	cameras, err := discovery.Discover()
	if err != nil {
		slog.Error("failed to discover cameras", "err", err)
	} else {
		keys := make([]string, 0, len(cameras))
		for k := range cameras {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		slog.Info("discovered cameras", "count", len(keys))
		for _, device := range keys {
			info := cameras[device]
			slog.Info("camera",
				"device", device,
				"card", info.Card,
				"driver", info.Driver,
				"bus_info", info.BusInfo,
				"version", info.Version,
				"capabilities", info.Capabilities,
				"device_caps", info.DeviceCaps,
			)
		}
	}

	detector := cameracoordinator.NewEBPFVb2IoctlStreamDetector()
	coord := cameracoordinator.NewCameraCoordinator(detector)

	var wg sync.WaitGroup

	// Run the coordinator in background; it logs errors itself and returns nil.
	wg.Add(1)
	go func() {
		defer wg.Done()
		slog.Info("detecting when camera recording starts/stops...")
		_ = coord.Run(ctx)
	}()

	defer wg.Wait()

	for {
		select {
		case <-ctx.Done():
			return
		case event, open := <-coord.Events():
			if !open {
				slog.Info("event channel closed, exiting")
				return
			}

			slog.Info("camera event",
				"time", time.Now().Format(time.RFC3339Nano),
				"event", event.Type.String(),
				"device", event.VideoFilename,
			)
		}
	}
}
