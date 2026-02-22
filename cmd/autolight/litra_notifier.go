package main

import (
	"context"
	"log/slog"
	"strings"

	lib "github.com/kharyam/go-litra-driver/lib"
	"github.com/shuhaowu/cameracoordinator"
)

// litraController abstracts the go-litra-driver/lib calls so that tests can
// supply a fake without touching USB hardware.
type litraController interface {
	ListDevices() []lib.DiscoveredDevice
	LightOn(deviceIndex int)
	LightOff(deviceIndex int)
}

// defaultLitraController is the production implementation of litraController.
type defaultLitraController struct{}

func (defaultLitraController) ListDevices() []lib.DiscoveredDevice {
	return lib.ListDevices()
}

func (defaultLitraController) LightOn(deviceIndex int) {
	lib.LightOn(deviceIndex)
}

func (defaultLitraController) LightOff(deviceIndex int) {
	lib.LightOff(deviceIndex)
}

// LitraNotifier is a cameracoordinator.Notifier that turns Litra lights on
// when any configured camera starts recording, and off when all of them stop.
type LitraNotifier struct {
	cameraNames []string
	lightNames  []string
	ctrl        litraController

	// activeDevices tracks the video devices that are currently recording and
	// passed the camera filter. Accesses are safe without synchronisation
	// because Run is a single goroutine reading a channel sequentially.
	activeDevices map[string]struct{}
}

// NewLitraNotifier creates a LitraNotifier from cfg. ctrl provides the
// underlying Litra device operations; pass a defaultLitraController{} for
// production use.
func NewLitraNotifier(cfg AppConfig, ctrl litraController) *LitraNotifier {
	return &LitraNotifier{
		cameraNames:   cfg.CameraNames,
		lightNames:    cfg.LightNames,
		ctrl:          ctrl,
		activeDevices: make(map[string]struct{}),
	}
}

// Run processes camera events until ctx is cancelled or events is closed.
func (n *LitraNotifier) Run(ctx context.Context, events <-chan cameracoordinator.CameraEvent) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case event, open := <-events:
			if !open {
				return nil
			}
			n.handle(event)
		}
	}
}

// handle applies the light-control logic for a single event.
func (n *LitraNotifier) handle(event cameracoordinator.CameraEvent) {
	switch event.Type {
	case cameracoordinator.CameraEventRecordingOn:
		if !n.cameraMatches(event.Capability.CardString()) {
			slog.Debug("autolight: camera not in filter, ignoring on event",
				"device", event.VideoDevice,
				"card", event.Capability.CardString(),
			)
			return
		}
		wasEmpty := len(n.activeDevices) == 0
		n.activeDevices[event.VideoDevice] = struct{}{}
		slog.Debug("autolight: camera recording started",
			"device", event.VideoDevice,
			"active_count", len(n.activeDevices),
		)
		if wasEmpty {
			n.turnLightsOn()
		}

	case cameracoordinator.CameraEventRecordingOff:
		if _, ok := n.activeDevices[event.VideoDevice]; !ok {
			// Device was not in our active set (filtered out on start, or
			// we never saw the on event), so there is nothing to do.
			return
		}
		delete(n.activeDevices, event.VideoDevice)
		slog.Debug("autolight: camera recording stopped",
			"device", event.VideoDevice,
			"active_count", len(n.activeDevices),
		)
		if len(n.activeDevices) == 0 {
			n.turnLightsOff()
		}

	default:
		slog.Warn("autolight: unknown event type, ignoring",
			"event", event.Type,
			"device", event.VideoDevice,
		)
	}
}

// cameraMatches returns true if card matches the camera name filter. An empty
// filter matches everything.
func (n *LitraNotifier) cameraMatches(card string) bool {
	if len(n.cameraNames) == 0 {
		return true
	}
	for _, name := range n.cameraNames {
		if strings.Contains(card, name) {
			return true
		}
	}
	return false
}

// lightMatches returns true if name matches the light name filter. An empty
// filter matches everything.
func (n *LitraNotifier) lightMatches(name string) bool {
	if len(n.lightNames) == 0 {
		return true
	}
	for _, ln := range n.lightNames {
		if strings.Contains(name, ln) {
			return true
		}
	}
	return false
}

// turnLightsOn turns on every configured Litra light.
func (n *LitraNotifier) turnLightsOn() {
	devices := n.ctrl.ListDevices()
	slog.Info("autolight: turning lights on", "discovered_lights", len(devices))
	for _, d := range devices {
		if n.lightMatches(d.Name) {
			slog.Debug("autolight: turning on light", "index", d.Index, "name", d.Name)
			n.ctrl.LightOn(d.Index)
		}
	}
}

// turnLightsOff turns off every configured Litra light.
func (n *LitraNotifier) turnLightsOff() {
	devices := n.ctrl.ListDevices()
	slog.Info("autolight: turning lights off", "discovered_lights", len(devices))
	for _, d := range devices {
		if n.lightMatches(d.Name) {
			slog.Debug("autolight: turning off light", "index", d.Index, "name", d.Name)
			n.ctrl.LightOff(d.Index)
		}
	}
}
