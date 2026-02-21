package cameracoordinator

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ScriptNotifierConfig holds the paths to scripts to run on camera events.
type ScriptNotifierConfig struct {
	// OnScript is the path to the script to run when recording starts.
	// If empty, no script is run.
	OnScript string

	// OffScript is the path to the script to run when recording stops.
	// If empty, no script is run.
	OffScript string
	// BaseDir is the directory where the JSON config file was located. If
	// a script path starts with "./" it will be resolved relative to this
	// directory.
	BaseDir string
}

// ScriptNotifier is a Notifier that executes shell scripts on camera recording
// on/off events.
type ScriptNotifier struct {
	cfg ScriptNotifierConfig

	// commandContext is used to create *exec.Cmd instances. For testing purposes
	commandContext func(ctx context.Context, name string, args ...string) *exec.Cmd
}

// NewScriptNotifier creates a ScriptNotifier with the given configuration.
func NewScriptNotifier(cfg ScriptNotifierConfig) *ScriptNotifier {
	return &ScriptNotifier{
		cfg:            cfg,
		commandContext: exec.CommandContext,
	}
}

// Run listens for CameraEvents and executes the configured scripts until ctx
// is cancelled or the events channel is closed.
func (s *ScriptNotifier) Run(ctx context.Context, events <-chan CameraEvent) error {
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

// handle runs the appropriate script for the given event.
func (s *ScriptNotifier) handle(ctx context.Context, event CameraEvent) {
	var script string
	switch event.Type {
	case CameraEventRecordingOn:
		script = s.cfg.OnScript
	case CameraEventRecordingOff:
		script = s.cfg.OffScript
	default:
		slog.Warn("script notifier: unknown event type, ignoring",
			"event", event.Type,
			"device", event.VideoDevice,
		)
		return
	}

	if script == "" {
		return
	}

	// If script path starts with "./" resolve it relative to the config
	// file directory so users can write "./on.sh" in their config.
	if strings.HasPrefix(script, "./") && s.cfg.BaseDir != "" {
		script = filepath.Join(s.cfg.BaseDir, script)
	}

	slog.Info("script notifier: running script",
		"script", script,
		"event", event.Type.String(),
		"device", event.VideoDevice,
	)

	cmd := s.commandContext(ctx, script, event.Type.String(), event.VideoDevice)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		slog.Error("script notifier: script failed",
			"script", script,
			"event", event.Type.String(),
			"device", event.VideoDevice,
			"err", err,
		)
	}
}
