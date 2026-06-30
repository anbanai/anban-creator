package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestVideoAPIConfigDefaultsAndEnvExpansion(t *testing.T) {
	t.Setenv("VOLCENGINE_ARK_API_KEY", "ark-test-key")

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	body := []byte(`
server: {}
database:
  dsn: "user:pass@tcp(localhost:3306)/anbanwriter"
jwt:
  secret_key: test-secret-key
mcp: {}
storage: {}
video_api:
  key: "${VOLCENGINE_ARK_API_KEY}"
  model: "doubao-seedance-2-0-test"
  timeout: 7m
  credits: 1234
  defaults:
    resolution: "720p"
    ratio: "16:9"
    duration: 10
    watermark: true
claude:
  plugin_dir: "../../claudecode"
`)
	if err := os.WriteFile(cfgPath, body, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := NewConfig(cfgPath)
	if err != nil {
		t.Fatalf("NewConfig() error = %v", err)
	}

	if cfg.VideoAPI.Key != "ark-test-key" {
		t.Fatalf("VideoAPI.Key = %q", cfg.VideoAPI.Key)
	}
	if cfg.VideoAPI.BaseURL != DefaultVideoAPIBaseURL {
		t.Fatalf("VideoAPI.BaseURL = %q, want %q", cfg.VideoAPI.BaseURL, DefaultVideoAPIBaseURL)
	}
	if cfg.VideoAPI.Model != "doubao-seedance-2-0-test" {
		t.Fatalf("VideoAPI.Model = %q", cfg.VideoAPI.Model)
	}
	if cfg.VideoAPI.Timeout != 7*time.Minute {
		t.Fatalf("VideoAPI.Timeout = %v", cfg.VideoAPI.Timeout)
	}
	if cfg.VideoAPI.Credits != 1234 {
		t.Fatalf("VideoAPI.Credits = %d", cfg.VideoAPI.Credits)
	}
	if cfg.VideoAPI.Defaults.Resolution != "720p" || cfg.VideoAPI.Defaults.Ratio != "16:9" || cfg.VideoAPI.Defaults.Duration != 10 {
		t.Fatalf("VideoAPI.Defaults = %#v", cfg.VideoAPI.Defaults)
	}
	if cfg.VideoAPI.Defaults.Watermark == nil || !*cfg.VideoAPI.Defaults.Watermark {
		t.Fatalf("VideoAPI.Defaults.Watermark = %#v", cfg.VideoAPI.Defaults.Watermark)
	}
}

func TestVideoAPIConfigAppliesShortVideoDefaults(t *testing.T) {
	cfg := &Config{}
	cfg.applyDefaults()

	if cfg.VideoAPI.BaseURL != DefaultVideoAPIBaseURL {
		t.Fatalf("VideoAPI.BaseURL = %q, want %q", cfg.VideoAPI.BaseURL, DefaultVideoAPIBaseURL)
	}
	if cfg.VideoAPI.Defaults.Resolution != "1080p" {
		t.Fatalf("default resolution = %q", cfg.VideoAPI.Defaults.Resolution)
	}
	if cfg.VideoAPI.Defaults.Ratio != "9:16" {
		t.Fatalf("default ratio = %q", cfg.VideoAPI.Defaults.Ratio)
	}
	if cfg.VideoAPI.Defaults.Duration != 15 {
		t.Fatalf("default duration = %d", cfg.VideoAPI.Defaults.Duration)
	}
	if cfg.VideoAPI.Timeout != 10*time.Minute {
		t.Fatalf("default timeout = %v", cfg.VideoAPI.Timeout)
	}
}
