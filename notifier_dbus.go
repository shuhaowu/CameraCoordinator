package cameracoordinator

import (
	"context"
	"errors"
	"log/slog"

	"github.com/godbus/dbus/v5"
)

const (
	// DBusServiceName is the well-known name claimed on the system bus.
	DBusServiceName = "io.github.shuhaowu.CameraCoordinator"

	// DBusObjectPath is the object path used when emitting signals.
	DBusObjectPath = dbus.ObjectPath("/io/github/shuhaowu/CameraCoordinator")

	// DBusInterface is the DBus interface name for signals emitted by the
	// notifier.
	DBusInterface = "io.github.shuhaowu.CameraCoordinator"

	// DBusSignalName is the fully-qualified signal name (interface.Signal).
	DBusSignalName = DBusInterface + ".CameraEvent"
)

// DBusNotifierSignalBody is the structured payload carried by every
// CameraEvent DBus signal.
type DBusNotifierSignalBody struct {
	Detector    string
	Type        uint32
	VideoDevice string
	Card        string
	BusInfo     string
}

// DBusNotifier is a Notifier that emits a CameraEvent signal on the system
// DBus bus whenever a camera recording starts or stops.  The notifier claims
// the well-known name DBusServiceName so that clients can filter by sender.
type DBusNotifier struct{}

// NewDBusNotifier creates a DBusNotifier.
func NewDBusNotifier() *DBusNotifier {
	return &DBusNotifier{}
}

// Run connects to the system bus, claims the well-known name, and listens for
// CameraEvents, emitting a DBus signal for each one until ctx is cancelled or
// the events channel is closed.
func (d *DBusNotifier) Run(ctx context.Context, events <-chan CameraEvent) error {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		slog.Error("dbus notifier: failed to connect to system bus", "err", err)
		return err
	}
	defer conn.Close()

	reply, err := conn.RequestName(DBusServiceName, dbus.NameFlagDoNotQueue)
	if err != nil {
		slog.Error("dbus notifier: failed to request bus name", "name", DBusServiceName, "err", err)
		return err
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		err = errors.New("dbus notifier: could not acquire well-known name, another instance may be running")
		slog.Error(err.Error(),
			"name", DBusServiceName,
			"reply", reply,
		)
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case event, open := <-events:
			if !open {
				return nil
			}

			body := DBusNotifierSignalBody{
				Detector:    event.Detector,
				Type:        uint32(event.Type),
				VideoDevice: event.VideoDevice,
				Card:        event.Card,
				BusInfo:     event.BusInfo,
			}

			if err := conn.Emit(DBusObjectPath, DBusSignalName, body); err != nil {
				slog.Error("dbus notifier: failed to emit signal",
					"err", err,
					"event", event.Type.String(),
					"device", event.VideoDevice,
				)
			} else {
				slog.Debug("dbus notifier: emitted signal",
					"event", event.Type.String(),
					"device", event.VideoDevice,
				)
			}
		}
	}
}
