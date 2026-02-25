package main

import (
	"encoding/json"
	"fmt"
	"io"
)

// AppConfig holds the JSON configuration for autolight.
type AppConfig struct {
	// CameraCards is a list of V4L2 card names to watch.
	// If empty, all cameras are watched.
	CameraCards []string `json:"camera_cards,omitempty"`

	// LightNames is a list of Litra device names to control.
	// If empty, all discovered Litra lights are controlled.
	LightNames []string `json:"light_names,omitempty"`

	// cached lookup sets for faster membership tests
	cameraCardsSet map[string]struct{} `json:"-"`
	lightNamesSet  map[string]struct{} `json:"-"`
}

// LoadConfig reads and parses an AppConfig from r.
func LoadConfig(r io.Reader) (AppConfig, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return AppConfig{}, fmt.Errorf("read config: %w", err)
	}

	var cfg AppConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return AppConfig{}, fmt.Errorf("parse config: %w", err)
	}

	// populate lookup sets for O(1) membership tests
	cfg.cameraCardsSet = make(map[string]struct{}, len(cfg.CameraCards))
	for _, c := range cfg.CameraCards {
		cfg.cameraCardsSet[c] = struct{}{}
	}

	cfg.lightNamesSet = make(map[string]struct{}, len(cfg.LightNames))
	for _, n := range cfg.LightNames {
		cfg.lightNamesSet[n] = struct{}{}
	}

	return cfg, nil
}

// matchesCamera returns true if the given card name should be tracked
// according to the config. An empty CameraCards list matches everything.
func (c AppConfig) matchesCamera(card string) bool {
	if len(c.cameraCardsSet) == 0 {
		return true
	}
	_, ok := c.cameraCardsSet[card]
	return ok
}

// matchesLight returns true if the given light name should be controlled
// according to the config. An empty LightNames list matches everything.
func (c AppConfig) matchesLight(name string) bool {
	if len(c.lightNamesSet) == 0 {
		return true
	}
	_, ok := c.lightNamesSet[name]
	return ok
}
