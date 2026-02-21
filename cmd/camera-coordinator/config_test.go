package main

import (
	"strings"
	"testing"

	"github.com/shuhaowu/cameracoordinator"
)

func TestLoadConfig(t *testing.T) {
	jsonStr := `{
        "detectors": {"ebpf_vb2_ioctl": {}},
        "adapters":  {"print": {"enabled": true}}
    }`
	cfg, err := LoadConfig(strings.NewReader(jsonStr))
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}

	if cfg.Detectors.EBPFVb2Ioctl.Enabled {
		t.Fatalf("expected detector disabled by default")
	}
	if !cfg.Adapters.Print.Enabled {
		t.Fatalf("expected adapter enabled by default")
	}
}

func TestBuildDetectorsAndAdapters(t *testing.T) {
	// empty config leaves everything disabled
	cfg := AppConfig{}

	dets := buildDetectors(cfg.Detectors)
	if len(dets) != 0 {
		t.Fatalf("expected 0 detectors with empty config, got %d", len(dets))
	}

	ads := buildAdapters(cfg.Adapters)
	if len(ads) != 0 {
		t.Fatalf("expected 0 adapters with empty config, got %d", len(ads))
	}

	// nil config also results in nothing enabled
	dets = buildDetectors(AppConfig{}.Detectors)
	if len(dets) != 0 {
		t.Fatalf("expected 0 detectors with nil config, got %d", len(dets))
	}
	ads = buildAdapters(AppConfig{}.Adapters)
	if len(ads) != 0 {
		t.Fatalf("expected 0 adapters with nil config, got %d", len(ads))
	}
}

func TestBuildDetectorsAndAdapters_ExplicitEnable(t *testing.T) {
	cfg := AppConfig{}
	cfg.Detectors.EBPFVb2Ioctl.Enabled = true
	cfg.Adapters.Print.Enabled = true

	dets := buildDetectors(cfg.Detectors)
	if len(dets) != 1 {
		t.Fatalf("expected 1 detector when explicitly enabled, got %d", len(dets))
	}
	if _, ok := dets[0].(*cameracoordinator.EBPFVb2IoctlStreamDetector); !ok {
		t.Fatalf("detector had wrong type: %T", dets[0])
	}

	ads := buildAdapters(cfg.Adapters)
	if len(ads) != 1 {
		t.Fatalf("expected 1 adapter when explicitly enabled, got %d", len(ads))
	}
	if _, ok := ads[0].(*cameracoordinator.PrintAdapter); !ok {
		t.Fatalf("adapter had wrong type: %T", ads[0])
	}
}

func TestDisabledEntries(t *testing.T) {
	cfg := AppConfig{}
	cfg.Detectors.EBPFVb2Ioctl.Enabled = false
	cfg.Adapters.Print.Enabled = false

	dets := buildDetectors(cfg.Detectors)
	if len(dets) != 0 {
		t.Fatalf("expected 0 detectors when disabled, got %d", len(dets))
	}
	ads := buildAdapters(cfg.Adapters)
	if len(ads) != 0 {
		t.Fatalf("expected 0 adapters when disabled, got %d", len(ads))
	}
}

// Unknown-component tests are no longer relevant since the schema is
// concrete; extra fields in the JSON are simply ignored by the decoder.  We
// still want to verify that an unrecognised detector/adaptor key doesn't
// cause a parse error.
func TestJSONAllowsUnknownFields(t *testing.T) {
	jsonStr := `{
        "detectors": {"foo": {}},
        "adapters":  {"bar": {}}
    }`
	if _, err := LoadConfig(strings.NewReader(jsonStr)); err != nil {
		t.Fatalf("unexpected parse error for unknown fields: %v", err)
	}
}
