package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/godbus/dbus/v5"
	"github.com/kharyam/go-litra-driver/lib"
	"github.com/shuhaowu/cameracoordinator"
)

// Use DBus constants from the shared package.

func main() {
	verbose := flag.Bool("verbose", false, "enable debug logging")
	configPath := flag.String("config", "", "path to JSON configuration file")
	flag.Parse()

	level := slog.LevelInfo
	if *verbose {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	var cfg AppConfig
	if *configPath != "" {
		f, err := os.Open(*configPath)
		if err != nil {
			slog.Error("failed to open config file", "path", *configPath, "err", err)
			os.Exit(1)
		}
		cfg, err = LoadConfig(f)
		f.Close()
		if err != nil {
			slog.Error("failed to load config", "path", *configPath, "err", err)
			os.Exit(1)
		}
	}

	slog.Info("autolight starting",
		"camera_cards", cfg.CameraCards,
		"light_names", cfg.LightNames,
	)

	if err := run(ctx, cfg); err != nil {
		slog.Error("fatal error", "err", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, cfg AppConfig) error {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return err
	}
	defer conn.Close()

	matchRule := dbus.WithMatchInterface(cameracoordinator.DBusInterface)
	matchMember := dbus.WithMatchMember(cameracoordinator.DBusMemberName)
	matchPath := dbus.WithMatchObjectPath(cameracoordinator.DBusObjectPath)

	if err := conn.AddMatchSignal(matchRule, matchMember, matchPath); err != nil {
		return err
	}

	sigCh := make(chan *dbus.Signal, 16)
	conn.Signal(sigCh)

	// activeCameras tracks which video devices are currently recording.
	// Lights are turned on when the map goes from empty -> non-empty,
	// and turned off when it goes from non-empty -> empty.
	activeCameras := make(map[string]struct{})

	slog.Info("listening for CameraEvent signals on system bus")

	for {
		select {
		case <-ctx.Done():
			slog.Info("shutting down")
			return nil
		case sig, ok := <-sigCh:
			if !ok {
				return nil
			}
			handleSignal(sig, &cfg, activeCameras)
		}
	}
}

func handleSignal(sig *dbus.Signal, cfg *AppConfig, activeCameras map[string]struct{}) {
	if len(sig.Body) < 1 {
		slog.Warn("received signal with empty body, ignoring")
		return
	}

	// The signal body is a single DBus struct, so sig.Body is []interface{} with one
	// element that is itself []interface{} containing the struct fields:
	//   Detector (string), Type (uint32), VideoDevice (string), Card (string), BusInfo (string)
	fields, ok := sig.Body[0].([]interface{})
	if !ok {
		slog.Warn("unexpected signal body format", "body", sig.Body)
		return
	}

	var body cameracoordinator.DBusNotifierSignalBody

	if err := dbus.Store(fields, &body.Detector, &body.Type, &body.VideoDevice, &body.Card, &body.BusInfo); err != nil {
		slog.Warn("failed to decode signal body", "err", err, "body", sig.Body)
		return
	}

	slog.Debug("received camera event",
		"type", body.Type,
		"video_device", body.VideoDevice,
		"card", body.Card,
		"bus_info", body.BusInfo,
	)

	if !cfg.matchesCamera(body.Card) {
		slog.Debug("ignoring event for non-matching camera", "card", body.Card)
		return
	}

	switch body.Type {
	case uint32(cameracoordinator.CameraEventRecordingOn):
		wasEmpty := len(activeCameras) == 0
		activeCameras[body.VideoDevice] = struct{}{}

		if wasEmpty {
			slog.Info("camera recording started, turning lights on", "video_device", body.VideoDevice, "card", body.Card)
			setLights(cfg, true)
		} else {
			slog.Info("additional camera started recording", "video_device", body.VideoDevice, "card", body.Card, "active_count", len(activeCameras))
			// Set lights anyways to make sure it is on if it turned off or didn't turn on the first time?
			setLights(cfg, true)
		}

	case uint32(cameracoordinator.CameraEventRecordingOff):
		delete(activeCameras, body.VideoDevice)

		if len(activeCameras) == 0 {
			slog.Info("all cameras stopped recording, turning lights off", "video_device", body.VideoDevice, "card", body.Card)
			setLights(cfg, false)
		} else {
			slog.Info("camera stopped but others still active", "video_device", body.VideoDevice, "card", body.Card, "active_count", len(activeCameras))
		}

	default:
		slog.Warn("unknown event type", "type", body.Type)
	}
}

func setLights(cfg *AppConfig, on bool) {
	devices := lib.ListDevices()
	if len(devices) == 0 {
		slog.Warn("no Litra devices found")
		return
	}

	for _, d := range devices {
		if !cfg.matchesLight(d.Name) {
			slog.Debug("skipping non-matching light", "name", d.Name, "index", d.Index)
			continue
		}

		if on {
			slog.Info("turning on light", "name", d.Name, "index", d.Index)
			lib.LightOn(d.Index)
		} else {
			slog.Info("turning off light", "name", d.Name, "index", d.Index)
			lib.LightOff(d.Index)
		}
	}
}
