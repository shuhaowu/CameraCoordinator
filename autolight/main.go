package main

import (
	"context"
	"flag"
	"fmt"
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

const disableLightControlLabel = "Disable auto light control"
const enableLightControlLabel = "Enable auto light control"

const cameraStatusOffLabel = "Camera: Off"
const cameraStatusOnLabel = "Camera: On"
const cameraStatusUnknownLabel = "Camera: Unknown"

const toggleLightOnLabel = "Turn Light On"
const toggleLightOffLabel = "Turn Light Off"

const brightnessMenuLabel = "Brightness"
const colorTempMenuLabel = "Color Temperature"

var brightnessPresets = []int{0, 25, 50, 75, 100}
var colorTempPresets = []int{2700, 3500, 4500, 5500, 6500}

type App struct {
	ctx    context.Context
	cancel context.CancelFunc

	mut      sync.Mutex
	disabled bool

	activeCameras map[string]struct{}

	conn *dbus.Conn

	trayRoot         *tray.Item
	cameraStatusItem *tray.MenuItem
	toggleLightItem  *tray.MenuItem
	enableItem       *tray.MenuItem
	quitItem         *tray.MenuItem

	lightOn bool
}

func NewApp(ctx context.Context) (*App, error) {
	ctx, cancel := context.WithCancel(ctx)
	app := &App{
		ctx:           ctx,
		cancel:        cancel,
		mut:           sync.Mutex{},
		activeCameras: make(map[string]struct{}),
	}

	root, err := tray.New(
		tray.ItemID("io.github.shuhaowu.autolight"),
		tray.ItemTitle("Autolight"),
		tray.ItemIconPixmap(cameraUnknownIcons...),
		tray.ItemToolTip("", nil, "Autolight", "Camera state is unknown. Turn a camera on or off to set it."),
		tray.ItemIsMenu(true),
	)
	if err != nil {
		return nil, err
	}

	menu := root.Menu()

	cameraStatusItem, err := menu.AddChild(
		tray.MenuItemLabel(cameraStatusUnknownLabel),
	)
	if err != nil {
		return nil, err
	}

	enableItem, err := menu.AddChild(
		tray.MenuItemLabel(disableLightControlLabel),
		tray.MenuItemHandler(tray.ClickedHandler(app.onEnableClicked)),
	)
	if err != nil {
		return nil, err
	}

	menu.AddChild(tray.MenuItemType(tray.Separator))

	toggleLightItem, err := menu.AddChild(
		tray.MenuItemLabel(toggleLightOnLabel),
		tray.MenuItemHandler(tray.ClickedHandler(app.onToggleLightClicked)),
	)
	if err != nil {
		return nil, err
	}

	brightnessItem, err := menu.AddChild(
		tray.MenuItemLabel(brightnessMenuLabel),
	)
	if err != nil {
		return nil, err
	}

	for _, level := range brightnessPresets {
		value := level
		_, err = brightnessItem.AddChild(
			tray.MenuItemLabel(fmt.Sprintf("%d%%", value)),
			tray.MenuItemHandler(tray.ClickedHandler(func(data any, timestamp uint32) error {
				app.mut.Lock()
				defer app.mut.Unlock()
				app.setBrightness(value)
				return nil
			})),
		)
		if err != nil {
			return nil, err
		}
	}

	colorTempItem, err := menu.AddChild(
		tray.MenuItemLabel(colorTempMenuLabel),
	)
	if err != nil {
		return nil, err
	}

	for _, temp := range colorTempPresets {
		value := temp
		_, err = colorTempItem.AddChild(
			tray.MenuItemLabel(fmt.Sprintf("%dk", value)),
			tray.MenuItemHandler(tray.ClickedHandler(func(data any, timestamp uint32) error {
				app.mut.Lock()
				defer app.mut.Unlock()
				app.setColorTemperature(value)
				return nil
			})),
		)
		if err != nil {
			return nil, err
		}
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
	app.cameraStatusItem = cameraStatusItem
	app.toggleLightItem = toggleLightItem
	app.enableItem = enableItem
	app.quitItem = quitItem

	return app, nil
}

func (a *App) onToggleLightClicked(data any, timestamp uint32) error {
	a.mut.Lock()
	defer a.mut.Unlock()

	a.setLights(!a.lightOn)
	return nil
}

func (a *App) onEnableClicked(data any, timestamp uint32) error {
	a.mut.Lock()
	defer a.mut.Unlock()

	a.disabled = !a.disabled

	var label string
	if a.disabled {
		label = enableLightControlLabel
	} else {
		label = disableLightControlLabel
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

	switch body.Type {
	case uint32(cameracoordinator.CameraEventRecordingOn):
		wasEmpty := len(a.activeCameras) == 0
		a.activeCameras[body.VideoDevice] = struct{}{}

		if wasEmpty {
			slog.Info("camera recording started, turning lights on", "video_device", body.VideoDevice, "card", body.Card)
			a.handleCameraOn()
		} else {
			slog.Info("additional camera started recording", "video_device", body.VideoDevice, "card", body.Card, "active_count", len(a.activeCameras))
			// We call handleCameraOn for every camera to try to converge the state if it somehow got corrupted before.
			a.handleCameraOn()
		}

	case uint32(cameracoordinator.CameraEventRecordingOff):
		delete(a.activeCameras, body.VideoDevice)

		if len(a.activeCameras) == 0 {
			slog.Info("all cameras stopped recording, turning lights off", "video_device", body.VideoDevice, "card", body.Card)
			a.handleCameraOff()
		} else {
			slog.Info("camera stopped but others still active", "video_device", body.VideoDevice, "card", body.Card, "active_count", len(a.activeCameras))
		}

	default:
		slog.Warn("unknown event type", "type", body.Type)
	}
}

func (a *App) handleCameraOn() {
	a.mut.Lock()
	defer a.mut.Unlock()

	a.trayRoot.SetProps(
		tray.ItemIconPixmap(cameraOnIcons...),
		tray.ItemToolTip("", nil, "Autolight", "Camera is on."),
	)

	a.cameraStatusItem.SetProps(tray.MenuItemLabel(cameraStatusOnLabel))

	if !a.disabled {
		a.setLights(true)
	}
}

func (a *App) handleCameraOff() {
	a.mut.Lock()
	defer a.mut.Unlock()

	a.trayRoot.SetProps(
		tray.ItemIconPixmap(cameraOffIcons...),
		tray.ItemToolTip("", nil, "Autolight", "Camera is off."),
	)

	a.cameraStatusItem.SetProps(tray.MenuItemLabel(cameraStatusOffLabel))

	if !a.disabled {
		a.setLights(false)
	}
}

func (a *App) setLights(on bool) {
	a.lightOn = on

	var toggleLabel string
	if on {
		toggleLabel = toggleLightOffLabel
	} else {
		toggleLabel = toggleLightOnLabel
	}
	a.toggleLightItem.SetProps(tray.MenuItemLabel(toggleLabel))

	devices := lib.ListDevices()
	if len(devices) == 0 {
		slog.Warn("no Litra devices found")
		return
	}

	for _, d := range devices {
		if on {
			slog.Info("turning on light", "name", d.Name, "index", d.Index)
			lib.LightOn(d.Index)
		} else {
			slog.Info("turning off light", "name", d.Name, "index", d.Index)
			lib.LightOff(d.Index)
		}
	}
}

func (a *App) setBrightness(level int) {
	devices := lib.ListDevices()
	if len(devices) == 0 {
		slog.Warn("no Litra devices found")
		return
	}

	slog.Info("setting brightness", "level", level)
	for _, d := range devices {
		slog.Debug("setting brightness on device", "name", d.Name, "index", d.Index, "level", level)
		lib.LightBrightness(d.Index, level)
	}
}

func (a *App) setColorTemperature(temp int) {
	devices := lib.ListDevices()
	if len(devices) == 0 {
		slog.Warn("no Litra devices found")
		return
	}

	slog.Info("setting color temperature", "temperature", temp)
	for _, d := range devices {
		slog.Debug("setting color temperature on device", "name", d.Name, "index", d.Index, "temp", temp)
		lib.LightTemperature(d.Index, uint16(temp))
	}
}

func (a *App) Close() {
	a.trayRoot.Close()
}

func main() {
	verbose := flag.Bool("verbose", false, "enable debug logging")
	versionFlag := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *versionFlag {
		fmt.Println(Version)
		return
	}

	level := slog.LevelInfo
	if *verbose {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	app, err := NewApp(ctx)
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
