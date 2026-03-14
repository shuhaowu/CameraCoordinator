package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"deedles.dev/tray"
	"github.com/godbus/dbus/v5"
	"github.com/kharyam/go-litra-driver/lib"
	"github.com/shuhaowu/cameracoordinator"
)

type App struct {
	ctx    context.Context
	cancel context.CancelFunc

	mut     sync.Mutex
	enabled bool

	cfg           AppConfig
	activeCameras map[string]struct{}

	conn *dbus.Conn

	trayRoot   *tray.Item
	enableItem *tray.MenuItem
	quitItem   *tray.MenuItem

	cameraItems map[string]*tray.MenuItem
	lightsItems map[string]*tray.MenuItem
}

func NewApp(ctx context.Context, cfg AppConfig) (*App, error) {
	ctx, cancel := context.WithCancel(ctx)
	app := &App{
		ctx:           ctx,
		cancel:        cancel,
		mut:           sync.Mutex{},
		cfg:           cfg,
		activeCameras: make(map[string]struct{}),
	}

	root, err := tray.New(
		tray.ItemID("io.github.shuhaowu.autolight"),
		tray.ItemTitle("Autolight"),
		tray.ItemIconName("webcam"),
		tray.ItemOverlayIconName("emblem-important"),
		tray.ItemToolTip("", nil, "Autolight", "Automatically control lights based on camera usage."),
		tray.ItemIsMenu(true),
	)
	if err != nil {
		return nil, err
	}

	menu := root.Menu()

	enableItem, err := menu.AddChild(
		tray.MenuItemLabel("Enable"),
		tray.MenuItemHandler(tray.ClickedHandler(app.onEnableClicked)),
	)
	if err != nil {
		return nil, err
	}

	menu.AddChild(tray.MenuItemType(tray.Separator))

	quitItem, err := menu.AddChild(
		tray.MenuItemLabel("Quit"),
		tray.MenuItemHandler(tray.ClickedHandler(app.onQuitClicked)),
	)
	if err != nil {
		return nil, err
	}

	app.trayRoot = root
	app.enableItem = enableItem
	app.quitItem = quitItem

	return app, nil
}

func (a *App) onEnableClicked(data any, timestamp uint32) error {
	a.mut.Lock()
	defer a.mut.Unlock()

	a.enabled = !a.enabled

	var label string
	if a.enabled {
		label = "Disable"
	} else {
		label = "Enable"
	}

	a.enableItem.SetProps(tray.MenuItemLabel(label))

	return nil
}

func (a *App) onQuitClicked(data any, timestamp uint32) error {
	a.cancel()
	return nil
}

func (a *App) Run() error {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return err
	}
	defer conn.Close()
	a.conn = conn

	matchRule := dbus.WithMatchInterface(cameracoordinator.DBusInterface)
	matchMember := dbus.WithMatchMember(cameracoordinator.DBusMemberName)
	matchPath := dbus.WithMatchObjectPath(cameracoordinator.DBusObjectPath)

	if err := conn.AddMatchSignal(matchRule, matchMember, matchPath); err != nil {
		return err
	}

	sigCh := make(chan *dbus.Signal, 16)
	conn.Signal(sigCh)

	slog.Info("listening for CameraEvent signals on system bus")

	for {
		select {
		case <-a.ctx.Done():
			slog.Info("shutting down")
			return nil
		case sig, ok := <-sigCh:
			if !ok {
				return nil
			}
			a.handleSignal(sig)
		}
	}
}

func (a *App) handleSignal(sig *dbus.Signal) {
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

	if !a.cfg.matchesCamera(body.Card) {
		slog.Debug("ignoring event for non-matching camera", "card", body.Card)
		return
	}

	switch body.Type {
	case uint32(cameracoordinator.CameraEventRecordingOn):
		wasEmpty := len(a.activeCameras) == 0
		a.activeCameras[body.VideoDevice] = struct{}{}

		if wasEmpty {
			slog.Info("camera recording started, turning lights on", "video_device", body.VideoDevice, "card", body.Card)
			a.setLights(true)
		} else {
			slog.Info("additional camera started recording", "video_device", body.VideoDevice, "card", body.Card, "active_count", len(a.activeCameras))
			a.setLights(true)
		}

	case uint32(cameracoordinator.CameraEventRecordingOff):
		delete(a.activeCameras, body.VideoDevice)

		if len(a.activeCameras) == 0 {
			slog.Info("all cameras stopped recording, turning lights off", "video_device", body.VideoDevice, "card", body.Card)
			a.setLights(false)
		} else {
			slog.Info("camera stopped but others still active", "video_device", body.VideoDevice, "card", body.Card, "active_count", len(a.activeCameras))
		}

	default:
		slog.Warn("unknown event type", "type", body.Type)
	}
}

func (a *App) setLights(on bool) {
	a.mut.Lock()
	enabled := a.enabled
	a.mut.Unlock()

	if !enabled {
		slog.Debug("lights control disabled, skipping", "on", on)
		return
	}

	devices := lib.ListDevices()
	if len(devices) == 0 {
		slog.Warn("no Litra devices found")
		return
	}

	for _, d := range devices {
		if !a.cfg.matchesLight(d.Name) {
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

func (a *App) Close() {
	a.trayRoot.Close()
}

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

	app, err := NewApp(ctx, cfg)
	if err != nil {
		slog.Error("failed to create app", "err", err)
		os.Exit(1)
	}

	defer app.Close()
	if err := app.Run(); err != nil {
		slog.Error("fatal error", "err", err)
		os.Exit(1)
	}
}
