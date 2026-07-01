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
  timeout: 7m
  credit_multiplier: 1200
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
	if cfg.VideoAPI.Timeout != 7*time.Minute {
		t.Fatalf("VideoAPI.Timeout = %v", cfg.VideoAPI.Timeout)
	}
	if cfg.VideoAPI.CreditMultiplier != 1200 {
		t.Fatalf("VideoAPI.CreditMultiplier = %d", cfg.VideoAPI.CreditMultiplier)
	}
}

func TestVideoAPIConfigAppliesGlobalDefaultsOnly(t *testing.T) {
	cfg := &Config{}
	cfg.applyDefaults()

	if cfg.VideoAPI.BaseURL != DefaultVideoAPIBaseURL {
		t.Fatalf("VideoAPI.BaseURL = %q, want %q", cfg.VideoAPI.BaseURL, DefaultVideoAPIBaseURL)
	}
	if cfg.VideoAPI.Timeout != 10*time.Minute {
		t.Fatalf("default timeout = %v", cfg.VideoAPI.Timeout)
	}
	if cfg.VideoAPI.CreditMultiplier != 1000 {
		t.Fatalf("default credit multiplier = %d", cfg.VideoAPI.CreditMultiplier)
	}
}
