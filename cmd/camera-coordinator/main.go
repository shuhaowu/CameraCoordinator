package main

import (
	"context"
	"encoding/json"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"

	"github.com/shuhaowu/cameracoordinator"
)

// defaultConfig contains the built-in configuration used when no external
// file is provided.  It enables the EBPF vb2_ioctl detector and the print
// notifier.
var defaultConfig = AppConfig{
	Detectors: DetectorConfig{
		EBPFVb2Ioctl: struct {
			Enabled bool `json:"enabled,omitempty"`
		}{Enabled: true},
	},
	Notifiers: NotifierConfig{
		Print: struct {
			Enabled bool `json:"enabled,omitempty"`
		}{Enabled: true},
	},
}

func main() {
	// parse command-line options early so we can configure logging
	verbose := flag.Bool("verbose", false, "enable debug logging")
	configPath := flag.String("config", "", "path to JSON configuration file defining detectors and notifiers")
	flag.Parse()

	// set default slog level based on verbose flag
	level := slog.LevelInfo
	if *verbose {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// prepare configuration; if no path is specified we fall back to the
	// built-in default that enables ebpf and print notifiers.
	cfg := defaultConfig
	configDir := ""
	if *configPath != "" {
		f, err := os.Open(*configPath)
		if err != nil {
			slog.Error("failed to open config file", "path", *configPath, "err", err)
			os.Exit(1)
		}
		defer f.Close()

		loaded, err := LoadConfig(f)
		if err != nil {
			slog.Error("failed to load config", "path", *configPath, "err", err)
			os.Exit(1)
		}
		cfg = loaded
		// record directory of provided config so notifiers can resolve ./ paths
		configDir = filepath.Dir(*configPath)
		// log raw config for debugging
		if data, err := json.Marshal(cfg); err == nil {
			slog.Debug("parsed config", "config", string(data))
		}
	}

	// build detectors and notifiers from configuration (or fall back to defaults)
	// build detectors always from cfg value
	detectors := buildDetectors(cfg.Detectors)

	if len(detectors) == 0 {
		slog.Error("no detectors enabled in configuration")
		os.Exit(1)
	}

	coord := cameracoordinator.NewCameraCoordinator(detectors...)

	// build notifiers always from cfg value
	notifiers := buildNotifiers(cfg.Notifiers, configDir)
	if len(notifiers) == 0 {
		slog.Error("no notifiers enabled in configuration")
		os.Exit(1)
	}

	// we still want to log events, but now go through an EventBroadcaster so
	// we can easily add additional notifiers later without touching the
	// coordinator.
	broadcaster := cameracoordinator.NewEventBroadcaster(len(notifiers), 1) // one output per notifier, small buffer

	var wg sync.WaitGroup

	// Run the coordinator in background; it logs errors itself and returns nil.
	wg.Add(1)
	go func() {
		defer wg.Done()
		slog.Info("detecting when camera recording starts/stops...")
		_ = coord.Run(ctx)
	}()

	// Run the broadcaster which implements Notifier and sits between the
	// coordinator and the configured notifiers.  Each notifier will read from
	// one of the broadcaster's output channels.
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = broadcaster.Run(ctx, coord.Events())
	}()

	// Run each configured notifier against its broadcaster output channel.
	for i, notifier := range notifiers {
		wg.Add(1)
		idx := i
		notif := notifier
		go func() {
			defer wg.Done()
			_ = notif.Run(ctx, broadcaster.Channel(idx))
		}()
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
