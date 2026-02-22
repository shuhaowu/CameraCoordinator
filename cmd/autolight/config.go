package main

import (
	"encoding/json"
	"fmt"
	"io"
)

// AppConfig is the top-level configuration for the autolight binary.
type AppConfig struct {
	// CameraNames is a list of substrings matched against the V4L2
	// CardString of a video device (e.g. "Logitech BRIO"). A device matches
	// if its CardString contains any of the listed substrings. An empty list
	// matches every camera.
	CameraNames []string `json:"camera_names,omitempty"`

	// LightNames is a list of substrings matched against the Name field of a
	// discovered Litra device. A device matches if its Name contains any of
	// the listed substrings. An empty list matches every light.
	LightNames []string `json:"light_names,omitempty"`
}

// LoadConfig reads and parses an AppConfig from r. Unknown JSON fields are
// rejected so that typos in the config file are caught at startup.
func LoadConfig(r io.Reader) (AppConfig, error) {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()

	var cfg AppConfig
	if err := dec.Decode(&cfg); err != nil {
		return AppConfig{}, fmt.Errorf("parse config: %w", err)
	}
	return cfg, nil
}
