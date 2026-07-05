package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestVideoGenerationRouteConfigDefaultsAndEnvExpansion(t *testing.T) {
	t.Setenv("VOLCENGINE_ARK_API_KEY", "ark-test-key")

	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "plugin")
	if err := os.MkdirAll(filepath.Join(pluginDir, "agents"), 0755); err != nil {
		t.Fatalf("mkdir plugin agents: %v", err)
	}
	cfgPath := filepath.Join(dir, "config.yaml")
	body := []byte(`
server: {}
database:
  dsn: "user:pass@tcp(localhost:3306)/anban-creator"
jwt:
  secret_key: test-secret-key
mcp: {}
storage: {}
model_providers:
  volcengine_ark:
    protocol: openai_compatible
    base_url: "https://ark.cn-beijing.volces.com/api/v3"
    api_key: "${VOLCENGINE_ARK_API_KEY}"
model_routes:
  video_generation:
    provider: volcengine_ark
    timeout: 7m
    model_catalog:
      - key: seedance-test
        display_name: Seedance Test
        model: doubao-seedance-test
        supported_resolutions: ["720p"]
        supported_ratios: ["9:16"]
        min_duration: 1
        max_duration: 15
        supports_video_input: true
model_prices:
  video_generation:
    volcengine_ark/doubao-seedance-test:
      currency: CNY
      no_input_price_per_second:
        720p: 0.8
      video_input_5s_min_price:
        720p: 4.28
      video_input_5s_max_price:
        720p: 9.50
claude:
  plugin_dir: "` + pluginDir + `"
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
	if got := cfg.VideoAPI.CreditMultiplierOrDefault(); got != 1000 {
		t.Fatalf("VideoAPI.CreditMultiplierOrDefault = %d", got)
	}
	if len(cfg.VideoAPI.ModelCatalog) != 1 {
		t.Fatalf("ModelCatalog length = %d", len(cfg.VideoAPI.ModelCatalog))
	}
	entry := cfg.VideoAPI.ModelCatalog[0]
	if entry.ModelID != "doubao-seedance-test" {
		t.Fatalf("ModelID = %q", entry.ModelID)
	}
	if entry.NoInputPricePerSecond["720p"] != 0.8 {
		t.Fatalf("NoInputPricePerSecond[720p] = %v", entry.NoInputPricePerSecond["720p"])
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
