package cameracoordinator

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
)

// ScriptAdapterConfig holds the paths to scripts to run on camera events.
type ScriptAdapterConfig struct {
	// OnScript is the path to the script to run when recording starts.
	// If empty, no script is run.
	OnScript string

	// OffScript is the path to the script to run when recording stops.
	// If empty, no script is run.
	OffScript string
}

// ScriptAdapter is an Adapter that executes shell scripts on camera recording
// on/off events.
type ScriptAdapter struct {
	cfg ScriptAdapterConfig
}

// NewScriptAdapter creates a ScriptAdapter with the given configuration.
func NewScriptAdapter(cfg ScriptAdapterConfig) *ScriptAdapter {
	return &ScriptAdapter{cfg: cfg}
}

// Run listens for CameraEvents and executes the configured scripts until ctx
// is cancelled or the events channel is closed.
func (s *ScriptAdapter) Run(ctx context.Context, events <-chan CameraEvent) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case event, open := <-events:
			if !open {
				return nil
			}

			// Run script in background so we don't block.
			go s.handle(ctx, event)
		}
	}
}

// commandContext is a variable wrapper around exec.CommandContext so tests
// can replace it with a fake implementation that records calls. Production
// code uses the real exec.CommandContext.
var commandContext = exec.CommandContext

// handle runs the appropriate script for the given event.
func (s *ScriptAdapter) handle(ctx context.Context, event CameraEvent) {
	var script string
	switch event.Type {
	case CameraEventRecordingOn:
		script = s.cfg.OnScript
	case CameraEventRecordingOff:
		script = s.cfg.OffScript
	default:
		slog.Warn("script adapter: unknown event type, ignoring",
			"event", event.Type,
			"device", event.VideoDevice,
		)
		return
	}

	if script == "" {
		return
	}

	slog.Info("script adapter: running script",
		"script", script,
		"event", event.Type.String(),
		"device", event.VideoDevice,
	)

	cmd := commandContext(ctx, script, event.Type.String(), event.VideoDevice)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		slog.Error("script adapter: script failed",
			"script", script,
			"event", event.Type.String(),
			"device", event.VideoDevice,
			"err", err,
		)
	}
}
