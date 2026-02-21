package main

import (
	"strings"
	"testing"

	"github.com/shuhaowu/cameracoordinator"
)

func TestLoadConfig(t *testing.T) {
	jsonStr := `{
        "detectors": {"ebpf_vb2_ioctl": {}},
        "notifiers":  {"print": {"enabled": true}}
    }`
	cfg, err := LoadConfig(strings.NewReader(jsonStr))
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}

	if cfg.Detectors.EBPFVb2Ioctl.Enabled {
		t.Fatalf("expected detector disabled by default")
	}
	if !cfg.Notifiers.Print.Enabled {
		t.Fatalf("expected notifier enabled by default")
	}
}

func TestBuildDetectorsAndNotifiers(t *testing.T) {
	// empty config leaves everything disabled
	cfg := AppConfig{}

	dets := buildDetectors(cfg.Detectors)
	if len(dets) != 0 {
		t.Fatalf("expected 0 detectors with empty config, got %d", len(dets))
	}

	ads := buildNotifiers(cfg.Notifiers, "")
	if len(ads) != 0 {
		t.Fatalf("expected 0 notifiers with empty config, got %d", len(ads))
	}

	// nil config also results in nothing enabled
	dets = buildDetectors(AppConfig{}.Detectors)
	if len(dets) != 0 {
		t.Fatalf("expected 0 detectors with nil config, got %d", len(dets))
	}
	ads = buildNotifiers(AppConfig{}.Notifiers, "")
	if len(ads) != 0 {
		t.Fatalf("expected 0 notifiers with nil config, got %d", len(ads))
	}
}

func TestBuildDetectorsAndNotifiers_ExplicitEnable(t *testing.T) {
	cfg := AppConfig{}
	cfg.Detectors.EBPFVb2Ioctl.Enabled = true
	cfg.Notifiers.Print.Enabled = true

	dets := buildDetectors(cfg.Detectors)
	if len(dets) != 1 {
		t.Fatalf("expected 1 detector when explicitly enabled, got %d", len(dets))
	}
	if _, ok := dets[0].(*cameracoordinator.EBPFVb2IoctlStreamDetector); !ok {
		t.Fatalf("detector had wrong type: %T", dets[0])
	}

	ads := buildNotifiers(cfg.Notifiers, "")
	if len(ads) != 1 {
		t.Fatalf("expected 1 notifier when explicitly enabled, got %d", len(ads))
	}
	if _, ok := ads[0].(*cameracoordinator.PrintNotifier); !ok {
		t.Fatalf("notifier had wrong type: %T", ads[0])
	}
}

func TestDisabledEntries(t *testing.T) {
	cfg := AppConfig{}
	cfg.Detectors.EBPFVb2Ioctl.Enabled = false
	cfg.Notifiers.Print.Enabled = false

	dets := buildDetectors(cfg.Detectors)
	if len(dets) != 0 {
		t.Fatalf("expected 0 detectors when disabled, got %d", len(dets))
	}
	ads := buildNotifiers(cfg.Notifiers, "")
	if len(ads) != 0 {
		t.Fatalf("expected 0 notifiers when disabled, got %d", len(ads))
	}
}

// Unknown-component tests are no longer relevant since the schema is
// concrete; extra fields in the JSON are simply ignored by the decoder.  We
// still want to verify that an unrecognised detector/notifier key doesn't
// cause a parse error.
func TestJSONAllowsUnknownFields(t *testing.T) {
	jsonStr := `{
        "detectors": {"foo": {}},
        "notifiers":  {"bar": {}}
    }`
	if _, err := LoadConfig(strings.NewReader(jsonStr)); err != nil {
		t.Fatalf("unexpected parse error for unknown fields: %v", err)
	}
}
