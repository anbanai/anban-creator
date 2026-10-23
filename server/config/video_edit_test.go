package config

import (
	"strings"
	"testing"
)

func TestVideoEditConfigDefaultsAndSecretRedaction(t *testing.T) {
	cfg := VideoEditConfig{}
	cfg.ApplyDefaults()
	if !cfg.Enabled || cfg.MaxAssets <= 0 || cfg.MaxAssetBytes != 256<<20 || cfg.MaxTotalInputBytes != 1<<30 || cfg.TimeoutMinutes <= 0 {
		t.Fatalf("defaults = %#v", cfg)
	}
	hasContentTags := false
	for _, target := range cfg.DefaultPolicy.DeliveryTargets {
		if target == "content_tags" {
			hasContentTags = true
			break
		}
	}
	if !hasContentTags {
		t.Fatalf("default delivery targets = %v, want required content_tags", cfg.DefaultPolicy.DeliveryTargets)
	}
	cfg.ElevenLabsAPIKey = "secret"
	if cfg.RedactedEnv()["ELEVENLABS_API_KEY"] != true {
		t.Fatalf("redacted env = %#v", cfg.RedactedEnv())
	}
}

func TestVideoEditConfigRequiresKeyOnlyAtDispatch(t *testing.T) {
	cfg := VideoEditConfig{Enabled: true}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate should not require a deployment secret: %v", err)
	}
	if cfg.DispatchReady() {
		t.Fatal("DispatchReady = true without ElevenLabs key")
	}
}

func TestVideoEditConfigRejectsLimitsAboveRuntimeContract(t *testing.T) {
	for _, cfg := range []VideoEditConfig{
		{Enabled: true, MaxAssetBytes: 256<<20 + 1},
		{Enabled: true, MaxTotalInputBytes: 1<<30 + 1},
	} {
		cfg.ApplyDefaults()
		if err := cfg.Validate(); err == nil {
			t.Fatalf("Validate(%#v) = nil, want size-limit rejection", cfg)
		}
	}
}

func TestVideoEditConfigLoadsSnakeCaseDefaultPolicy(t *testing.T) {
	body := validClaudeConfigYAML + `
video_edit:
  max_asset_bytes: 104857600
  max_total_input_bytes: 536870912
  default_policy:
    preferences:
      quality_tier: quality
      subtitle_mode: required
      max_duration_seconds: 240
    delivery_targets: [final_video]
`
	cfg, err := loadClaudeConfigYAML(t, strings.TrimSpace(body)+"\n")
	if err != nil {
		t.Fatalf("load video-edit default policy: %v", err)
	}
	defaults := cfg.VideoEdit.DefaultPolicy
	if cfg.VideoEdit.MaxAssetBytes != 100<<20 || cfg.VideoEdit.MaxTotalInputBytes != 512<<20 {
		t.Fatalf("video-edit limits = %d/%d, want configured values", cfg.VideoEdit.MaxAssetBytes, cfg.VideoEdit.MaxTotalInputBytes)
	}
	if defaults.Preferences.QualityTier != "quality" || defaults.Preferences.SubtitleMode != "required" || defaults.Preferences.MaxDuration != 240 {
		t.Fatalf("video-edit preferences = %#v, want configured values", defaults.Preferences)
	}
	if len(defaults.DeliveryTargets) != 1 || defaults.DeliveryTargets[0] != "final_video" {
		t.Fatalf("video-edit delivery targets = %v, want [final_video]", defaults.DeliveryTargets)
	}
}
