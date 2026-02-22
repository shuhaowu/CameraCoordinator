package main

import (
	"context"
	"encoding/json"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/shuhaowu/cameracoordinator"
)

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

	cfg := AppConfig{} // default: match all cameras and all lights
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
		if data, err := json.Marshal(cfg); err == nil {
			slog.Debug("parsed config", "config", string(data))
		}
	}

	detector := cameracoordinator.NewEBPFVb2IoctlStreamDetector()
	coord := cameracoordinator.NewCameraCoordinator(detector)

	notifier := NewLitraNotifier(cfg, defaultLitraController{})

	broadcaster := cameracoordinator.NewEventBroadcaster(1, 1)

	var wg sync.WaitGroup

	wg.Go(func() {
		slog.Info("autolight: detecting camera recording events...")
		_ = coord.Run(ctx)
	})

	wg.Go(func() {
		_ = broadcaster.Run(ctx, coord.Events())
	})

	wg.Go(func() {
		_ = notifier.Run(ctx, broadcaster.Channel(0))
	})

	wg.Wait()
}
