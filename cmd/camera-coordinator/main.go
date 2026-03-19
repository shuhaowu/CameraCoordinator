package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/shuhaowu/cameracoordinator"
)

func main() {
	// parse command-line options early so we can configure logging
	verbose := flag.Bool("verbose", false, "enable debug logging")
	onScript := flag.String("on-script", "", "script to run when camera turns on")
	offScript := flag.String("off-script", "", "script to run when camera turns off")
	versionFlag := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *versionFlag {
		fmt.Println(cameracoordinator.Version)
		return
	}

	// set default slog level based on verbose flag
	level := slog.LevelInfo
	if *verbose {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Build detectors: EBPF vb2_ioctl is always enabled.
	detectors := []cameracoordinator.CameraDetector{
		cameracoordinator.NewEBPFVb2IoctlStreamDetector(),
	}

	// Build notifiers: DBus is always enabled; Script notifier enabled only
	// if an on/off script is provided via flags.
	var notifiers []cameracoordinator.Notifier
	notifiers = append(notifiers, cameracoordinator.NewDBusNotifier())
	if *onScript != "" || *offScript != "" {

		cwd, err := os.Getwd()
		if err != nil {
			slog.Error("failed to get current working directory", "err", err)
			os.Exit(1)
			return
		}

		notifiers = append(notifiers, cameracoordinator.NewScriptNotifier(cameracoordinator.ScriptNotifierConfig{
			OnScript:  *onScript,
			OffScript: *offScript,
			BaseDir:   cwd,
		}))
	}

	// Create one output channel per notifier and wire them into the
	// coordinator. The coordinator fans out deduplicated events to all
	// channels directly. We close them after Run returns so downstream
	// notifiers can detect shutdown.
	notifierChannels := make([]chan cameracoordinator.CameraEvent, len(notifiers))
	for i := range notifierChannels {
		notifierChannels[i] = make(chan cameracoordinator.CameraEvent, 1)
	}

	coord := cameracoordinator.NewCameraCoordinator(notifierChannels)

	allEvents := make(chan cameracoordinator.CameraEvent)

	// Start each detector in its own goroutine. When all detectors have
	// finished, close allEvents so the coordinator knows to stop.
	var detectorWg sync.WaitGroup
	for _, det := range detectors {
		detectorWg.Go(func() {
			inner := slog.With("detector", det.Name())
			inner.Debug("starting detector")
			defer inner.Debug("stopping detector")
			if err := det.Run(ctx, allEvents); err != nil && !errors.Is(err, context.Canceled) {
				slog.Error("detector ran into an error", "detector", det.Name(), "err", err)
			}
		})
	}

	var wg sync.WaitGroup

	// Close allEvents once all detectors are done so the coordinator exits.
	wg.Go(func() {
		detectorWg.Wait()
		close(allEvents)
	})

	// Run the coordinator in background; it logs errors itself and returns nil.
	// Close notifier channels once the coordinator is done so all notifiers exit.
	wg.Go(func() {
		slog.Info("detecting when camera recording starts/stops...")
		_ = coord.Run(ctx, allEvents)
		for _, ch := range notifierChannels {
			close(ch)
		}
	})

	// Run each configured notifier against its coordinator output channel.
	for i, notifier := range notifiers {
		wg.Go(func() {
			_ = notifier.Run(ctx, notifierChannels[i])
		})
	}

	wg.Wait()
}

// func discoverAndLogCameras() {
// 	cameras, err := cameracoordinator.V4L2DiscoverDevices()
// 	if err != nil {
// 		slog.Error("failed to discover cameras", "err", err)
// 	} else {
// 		keys := make([]string, 0, len(cameras))
// 		for k := range cameras {
// 			keys = append(keys, k)
// 		}
// 		sort.Strings(keys)

// 		slog.Info("discovered cameras", "count", len(keys))
// 		for _, device := range keys {
// 			info := cameras[device]
// 			slog.Info("camera",
// 				"device", device,
// 				"card", info.CardString(),
// 				"driver", info.DriverString(),
// 				"bus_info", info.BusInfoString(),
// 				"version", info.VersionString(),
// 				"is_video_capture", info.HasCapabilities(cameracoordinator.V4L2CapVideoCapture),
// 				"capabilities", info.Capabilities,
// 				"device_caps", info.DeviceCaps,
// 			)
// 		}
// 	}
// }
