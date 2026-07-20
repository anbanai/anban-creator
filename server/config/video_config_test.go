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
claude:
  provider: volcengine_ark
  base_url: https://ark.cn-beijing.volces.com/api/compatible
  auth_token: test-auth-token
  models:
    default: doubao-seed-evolving
    opus: doubao-seed-evolving
    fable: doubao-seed-evolving
    sonnet: doubao-seed-2-1-pro-260628
    haiku: doubao-seed-2-1-turbo-260628
  model_usage_aliases:
    doubao-seed-evolving-latest-version: doubao-seed-evolving
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
	if len(cfg.VideoAPI.ModelCatalog) != 1 {
		t.Fatalf("ModelCatalog length = %d", len(cfg.VideoAPI.ModelCatalog))
	}
	entry := cfg.VideoAPI.ModelCatalog[0]
	if entry.ModelID != "doubao-seedance-test" {
		t.Fatalf("ModelID = %q", entry.ModelID)
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
}
