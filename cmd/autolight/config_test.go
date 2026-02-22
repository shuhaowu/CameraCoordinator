package main

import (
	"strings"
	"testing"
)

func TestLoadConfig_Full(t *testing.T) {
	// All fields populated; verify values are decoded correctly.
	raw := `{
		"camera_names": ["Logitech", "C920"],
		"light_names":  ["Litra Glow"]
	}`
	cfg, err := LoadConfig(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("LoadConfig returned unexpected error: %v", err)
	}
	if len(cfg.CameraNames) != 2 || cfg.CameraNames[0] != "Logitech" || cfg.CameraNames[1] != "C920" {
		t.Errorf("unexpected CameraNames: %v", cfg.CameraNames)
	}
	if len(cfg.LightNames) != 1 || cfg.LightNames[0] != "Litra Glow" {
		t.Errorf("unexpected LightNames: %v", cfg.LightNames)
	}
}

func TestLoadConfig_Empty(t *testing.T) {
	// An empty JSON object produces a zero-value config, which means all
	// cameras and all lights (the match-everything default).
	cfg, err := LoadConfig(strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("LoadConfig returned unexpected error: %v", err)
	}
	if len(cfg.CameraNames) != 0 {
		t.Errorf("expected empty CameraNames, got %v", cfg.CameraNames)
	}
	if len(cfg.LightNames) != 0 {
		t.Errorf("expected empty LightNames, got %v", cfg.LightNames)
	}
}

func TestLoadConfig_UnknownField(t *testing.T) {
	// Unknown JSON fields should be rejected to surface typos at startup.
	_, err := LoadConfig(strings.NewReader(`{"unknown_field": true}`))
	if err == nil {
		t.Fatal("expected error for unknown field, got nil")
	}
}

func TestLoadConfig_InvalidJSON(t *testing.T) {
	// Malformed JSON should produce a parse error.
	_, err := LoadConfig(strings.NewReader(`{not valid`))
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}
