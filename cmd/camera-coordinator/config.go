package main

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/shuhaowu/cameracoordinator"
)

// DetectorConfig holds per-detector configuration.
type DetectorConfig struct {
	EBPFVb2Ioctl struct {
		Enabled bool `json:"enabled,omitempty"`
	} `json:"ebpf_vb2_ioctl"`
}

// NotifierConfig holds per-notifier configuration.
type NotifierConfig struct {
	Print struct {
		Enabled bool `json:"enabled,omitempty"`
	} `json:"print"`
	Script struct {
		Enabled   bool   `json:"enabled,omitempty"`
		OnScript  string `json:"on_script,omitempty"`
		OffScript string `json:"off_script,omitempty"`
	} `json:"script"`
}

// AppConfig represents the JSON configuration for the camera-coordinator
// binary.
type AppConfig struct {
	Detectors DetectorConfig `json:"detectors"`
	Notifiers NotifierConfig `json:"notifiers"`
}

// LoadConfig reads and parses an AppConfig from the supplied reader.
func LoadConfig(r io.Reader) (AppConfig, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return AppConfig{}, fmt.Errorf("read config: %w", err)
	}
	var cfg AppConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return AppConfig{}, fmt.Errorf("parse config: %w", err)
	}
	return cfg, nil
}

// buildDetectors creates a slice of CameraDetector based on the configuration.
func buildDetectors(cfg DetectorConfig) []cameracoordinator.CameraDetector {
	var result []cameracoordinator.CameraDetector

	if cfg.EBPFVb2Ioctl.Enabled {
		result = append(result, cameracoordinator.NewEBPFVb2IoctlStreamDetector())
	}

	return result
}

// buildNotifiers is analogous to buildDetectors.
func buildNotifiers(cfg NotifierConfig, configDir string) []cameracoordinator.Notifier {
	var result []cameracoordinator.Notifier

	if cfg.Print.Enabled {
		result = append(result, cameracoordinator.NewPrintNotifier())
	}

	if cfg.Script.Enabled {
		result = append(result, cameracoordinator.NewScriptNotifier(cameracoordinator.ScriptNotifierConfig{
			OnScript:  cfg.Script.OnScript,
			OffScript: cfg.Script.OffScript,
			BaseDir:   configDir,
		}))
	}

	return result
}
