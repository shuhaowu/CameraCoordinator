package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"sort"
	"sync"
	"syscall"

	"github.com/shuhaowu/cameracoordinator"
)

func main() {
	// set default slog level to Debug
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})))

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	cameras, err := cameracoordinator.V4L2DiscoverDevices()
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
				"card", info.CardString(),
				"driver", info.DriverString(),
				"bus_info", info.BusInfoString(),
				"version", info.VersionString(),
				"is_video_capture", info.HasCapabilities(cameracoordinator.V4L2CapVideoCapture),
				"capabilities", info.Capabilities,
				"device_caps", info.DeviceCaps,
			)
		}
	}

	detector := cameracoordinator.NewEBPFVb2IoctlStreamDetector()
	coord := cameracoordinator.NewCameraCoordinator(detector)
	adapter := cameracoordinator.NewPrintAdapter()

	var wg sync.WaitGroup

	// Run the coordinator in background; it logs errors itself and returns nil.
	wg.Add(1)
	go func() {
		defer wg.Done()
		slog.Info("detecting when camera recording starts/stops...")
		_ = coord.Run(ctx)
	}()

	// Run the adapter in background to process and print events.
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = adapter.Run(ctx, coord.Events())
	}()

	defer wg.Wait()

	<-ctx.Done()
}
