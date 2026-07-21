package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestImageGenerationTimeoutDefaults(t *testing.T) {
	cfg := Config{ModelRoutes: ModelRoutesConfig{ImageGeneration: ImageGenerationRoutesConfig{
		Designer: map[string]ImageGenerationRouteConfig{"seedream": {}},
	}}}
	cfg.applyDefaults()
	if cfg.MCP.ToolTimeouts.GenerateImage != 10*time.Minute {
		t.Fatalf("generate_image timeout = %s, want 10m", cfg.MCP.ToolTimeouts.GenerateImage)
	}
	if cfg.ModelRoutes.ImageGeneration.Cover.Timeout != 5*time.Minute ||
		cfg.ModelRoutes.ImageGeneration.Content.Timeout != 5*time.Minute ||
		cfg.ModelRoutes.ImageGeneration.Designer["seedream"].Timeout != 5*time.Minute {
		t.Fatalf("image route timeouts = %s/%s/%s, want 5m/5m/5m",
			cfg.ModelRoutes.ImageGeneration.Cover.Timeout,
			cfg.ModelRoutes.ImageGeneration.Content.Timeout,
			cfg.ModelRoutes.ImageGeneration.Designer["seedream"].Timeout)
	}
}

func TestImageGenerationRouteTimeoutReachesRuntimeConfig(t *testing.T) {
	cfg := Config{
		ModelProviders: map[string]ModelProviderConfig{
			"volcengine_ark": {BaseURL: "https://ark.example.com", APIKey: "key"},
		},
		ModelRoutes: ModelRoutesConfig{
			ImageGeneration: ImageGenerationRoutesConfig{
				Cover: ImageGenerationRouteConfig{
					Provider: "volcengine_ark", Model: "seedream", Timeout: 2 * time.Minute,
				},
			},
		},
	}
	if err := cfg.deriveModelRouteRuntimeConfig(); err != nil {
		t.Fatalf("deriveModelRouteRuntimeConfig() error = %v", err)
	}
	if cfg.ImageAPI.Cover == nil || cfg.ImageAPI.Cover.TimeoutSec != 120 {
		t.Fatalf("cover runtime config = %#v, want timeout_sec 120", cfg.ImageAPI.Cover)
	}
}

func TestValidateRejectsImageTimeoutOutsideOperationBudget(t *testing.T) {
	cfg := baseKubernetesConfigForTest()
	cfg.MCP.ToolTimeouts.GenerateImage = 5 * time.Minute
	cfg.ModelRoutes.ImageGeneration.Cover = ImageGenerationRouteConfig{
		Provider: "volcengine_ark", Model: "seedream", Timeout: 5 * time.Minute,
	}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "mcp.tool_timeouts.generate_image") {
		t.Fatalf("Validate() error = %v, want timeout relationship error", err)
	}
}

func TestSemanticModelConfigRejectsDeprecatedVision(t *testing.T) {
	dir := t.TempDir()
	pluginDir := fakePluginDir(t, dir)
	cfgPath := filepath.Join(dir, "config.yaml")
	body := []byte(`
server: {}
database:
  dsn: "user:pass@tcp(localhost:3306)/creator"
jwt:
  secret_key: test-secret
vision:
  base_url: "https://api.example.com/v1"
  key: "sk-test"
  model: "old-vision"
claude:
  plugin_dir: "` + pluginDir + `"
`)
	if err := os.WriteFile(cfgPath, body, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := NewConfig(cfgPath)
	if err == nil {
		t.Fatal("NewConfig() succeeded, want deprecated vision error")
	}
	if !strings.Contains(err.Error(), "deprecated top-level config key vision") {
		t.Fatalf("error = %v, want deprecated vision hint", err)
	}
}

func TestSemanticModelConfigRejectsUnknownTopLevelKey(t *testing.T) {
	dir := t.TempDir()
	pluginDir := fakePluginDir(t, dir)
	cfgPath := filepath.Join(dir, "config.yaml")
	body := []byte(`
server: {}
database:
  dsn: "user:pass@tcp(localhost:3306)/creator"
jwt:
  secret_key: test-secret
image_api_typo:
  content:
    sizes:
      article_cover: "1:1"
claude:
  plugin_dir: "` + pluginDir + `"
`)
	if err := os.WriteFile(cfgPath, body, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := NewConfig(cfgPath)
	if err == nil {
		t.Fatal("NewConfig() succeeded, want unknown key error")
	}
	if !strings.Contains(err.Error(), "unknown top-level config key image_api_typo") {
		t.Fatalf("error = %v, want unknown key hint", err)
	}
}

func TestSemanticModelConfigDerivesRuntimeRoutes(t *testing.T) {
	t.Setenv("MOONSHOT_API_KEY", "moonshot-test")
	t.Setenv("VOLCENGINE_ARK_API_KEY", "ark-test")
	t.Setenv("WANGCAI_OPENAI_API_KEY", "wangcai-test")

	dir := t.TempDir()
	pluginDir := fakePluginDir(t, dir)
	cfgPath := filepath.Join(dir, "config.yaml")
	body := []byte(`
server: {}
database:
  dsn: "user:pass@tcp(localhost:3306)/creator"
jwt:
  secret_key: test-secret
model_providers:
  moonshot:
    protocol: openai_compatible
    base_url: https://api.moonshot.cn/v1
    api_key: "${MOONSHOT_API_KEY}"
  volcengine_ark:
    protocol: openai_compatible
    base_url: https://ark.cn-beijing.volces.com/api/v3
    api_key: "${VOLCENGINE_ARK_API_KEY}"
  wangcai_openai:
    protocol: openai_compatible
    base_url: http://18.141.196.64:18888/v1
    api_key: "${WANGCAI_OPENAI_API_KEY}"
model_routes:
  writing:
    provider: moonshot
    model: kimi-k2.7-code
    timeout: 5m
  image_understanding:
    provider: moonshot
    model: kimi-k2.7-code-highspeed
    timeout: 90s
    require_usage: true
  video_understanding:
    provider: moonshot
    model: kimi-k2.7-code-highspeed
    timeout: 180s
    require_usage: true
    require_native_video: true
  image_generation:
    cover:
      provider: volcengine_ark
      model: doubao-seedream-5-0-pro-260628
    content:
      provider: volcengine_ark
      model: doubao-seedream-5-0-pro-260628
    designer:
      seedream:
        alias: Doubao Seedream
        provider: volcengine_ark
        model: doubao-seedream-5-0-pro-260628
        enabled: true
        quality_rank: 100
        capabilities:
          size_presets: ["1:1", "16:9", "9:16", "4:3", "3:4", "3:2", "2:3", "21:9"]
          default_size: "1:1"
          max_batch: 1
          max_reference_images: 10
          supports_reference: true
          supports_mask: false
          output_formats: [png, jpeg]
          has_background: false
          has_compression: false
          watermark: true
      gpt_image_2:
        alias: GPT Image 2
        provider: wangcai_openai
        model: gpt-image-2
        enabled: true
        quality_rank: 200
        response_format: url
        capabilities:
          quality_levels: [auto, low, medium, high]
          size_presets: [auto, 1024x1024, 1536x1024, 1024x1536]
          default_size: auto
          max_batch: 10
          max_reference_images: 16
          supports_reference: true
          supports_mask: true
          output_formats: [png, jpeg, webp]
          has_background: true
          has_compression: true
image_presets:
  - key: openai-standard
    display_name: GPT Image 2
    provider_route: image_generation.designer.gpt_image_2
    min_tier: pro
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

	if cfg.Writing.Model != "kimi-k2.7-code" || cfg.Writing.BaseURL != "https://api.moonshot.cn/v1" {
		t.Fatalf("derived writing config = %#v", cfg.Writing)
	}
	if cfg.ImageUnderstanding.Model != "kimi-k2.7-code-highspeed" || !cfg.ImageUnderstanding.RequireUsage {
		t.Fatalf("image understanding route = %#v", cfg.ImageUnderstanding)
	}
	if cfg.VideoUnderstanding.Model != "kimi-k2.7-code-highspeed" || !cfg.VideoUnderstanding.RequireNativeVideo {
		t.Fatalf("video understanding route = %#v", cfg.VideoUnderstanding)
	}
	if cfg.ImageAPI.Cover == nil || cfg.ImageAPI.Cover.Provider != "volcengine" || cfg.ImageAPI.Cover.Model != "doubao-seedream-5-0-pro-260628" {
		t.Fatalf("derived image cover config = %#v", cfg.ImageAPI.Cover)
	}
	if len(cfg.ImagePresets) != 1 || cfg.ImagePresets[0].Provider != "openai" || cfg.ImagePresets[0].Endpoint != "http://18.141.196.64:18888/v1" || cfg.ImagePresets[0].APIKey != "wangcai-test" {
		t.Fatalf("derived image preset route = %#v", cfg.ImagePresets)
	}
	preset := cfg.ImagePresets[0]
	if preset.QualityRank != 200 || !preset.Capabilities.SupportsReference || preset.Capabilities.MaxReferenceImages != 16 {
		t.Fatalf("derived image preset capabilities = %#v", preset)
	}
	if preset.Timeout != 5*time.Minute {
		t.Fatalf("derived image preset timeout = %s, want 5m", preset.Timeout)
	}
	designerRoute := cfg.ModelRoutes.ImageGeneration.Designer["gpt_image_2"]
	if designerRoute.Capabilities.DefaultSize != "auto" {
		t.Fatalf("designer default size = %q, want auto", designerRoute.Capabilities.DefaultSize)
	}
	if got := designerRoute.Capabilities.SizePresets; len(got) != 4 || got[0] != "auto" || got[1] != "1024x1024" || got[2] != "1536x1024" || got[3] != "1024x1536" {
		t.Fatalf("designer size presets = %#v, want GPT Image official presets", got)
	}
	if designerRoute.Capabilities.MaxBatch != 10 || designerRoute.Capabilities.MaxReferenceImages != 16 || !designerRoute.Capabilities.SupportsMask {
		t.Fatalf("designer capabilities = %#v", designerRoute.Capabilities)
	}
	seedreamRoute := cfg.ModelRoutes.ImageGeneration.Designer["seedream"]
	if seedreamRoute.Capabilities.DefaultSize != "1:1" {
		t.Fatalf("seedream default size = %q, want 1:1", seedreamRoute.Capabilities.DefaultSize)
	}
	if got := seedreamRoute.Capabilities.SizePresets; len(got) != 8 || got[0] != "1:1" || got[5] != "3:2" || got[6] != "2:3" || got[7] != "21:9" {
		t.Fatalf("seedream size presets = %#v, want official Seedream 5.0 ratio presets", got)
	}
	if seedreamRoute.Capabilities.MaxBatch != 1 || seedreamRoute.Capabilities.MaxReferenceImages != 10 || !seedreamRoute.Capabilities.SupportsReference || seedreamRoute.Capabilities.SupportsMask {
		t.Fatalf("seedream capabilities = %#v", seedreamRoute.Capabilities)
	}
	if got := seedreamRoute.Capabilities.OutputFormats; len(got) != 2 || got[0] != "png" || got[1] != "jpeg" {
		t.Fatalf("seedream output formats = %#v, want png/jpeg", got)
	}
	if !seedreamRoute.Capabilities.Watermark {
		t.Fatalf("seedream watermark capability = false, want true")
	}
}

func TestSemanticModelConfigRejectsDesignerRoutesWithoutCapabilities(t *testing.T) {
	t.Setenv("WANGCAI_OPENAI_API_KEY", "wangcai-test")
	dir := t.TempDir()
	pluginDir := fakePluginDir(t, dir)

	for name, routeExtra := range map[string]string{
		"missing capabilities": "",
		"missing default size": `
        capabilities:
          size_presets: [auto, 1024x1024]
          max_batch: 1
`,
		"missing size presets": `
        capabilities:
          default_size: auto
          max_batch: 1
`,
	} {
		t.Run(name, func(t *testing.T) {
			cfgPath := filepath.Join(dir, strings.ReplaceAll(name, " ", "-")+".yaml")
			body := []byte(`
server: {}
database:
  dsn: "user:pass@tcp(localhost:3306)/creator"
jwt:
  secret_key: test-secret
model_providers:
  wangcai_openai:
    protocol: openai_compatible
    base_url: http://18.141.196.64:18888/v1
    api_key: "${WANGCAI_OPENAI_API_KEY}"
model_routes:
  image_generation:
    designer:
      gpt_image_2:
        alias: GPT Image 2
        provider: wangcai_openai
        model: gpt-image-2
        enabled: true
        quality_rank: 100
` + routeExtra + `
claude:
  plugin_dir: "` + pluginDir + `"
`)
			if err := os.WriteFile(cfgPath, body, 0644); err != nil {
				t.Fatalf("write config: %v", err)
			}

			_, err := NewConfig(cfgPath)
			if err == nil || !strings.Contains(err.Error(), "model_routes.image_generation.designer.gpt_image_2.capabilities") {
				t.Fatalf("error = %v, want designer capabilities validation error", err)
			}
		})
	}
}

func TestSemanticModelConfigRejectsEnabledDesignerRouteWithoutPositiveQualityRank(t *testing.T) {
	t.Setenv("WANGCAI_OPENAI_API_KEY", "wangcai-test")
	dir := t.TempDir()
	pluginDir := fakePluginDir(t, dir)
	cfgPath := filepath.Join(dir, "missing-quality-rank.yaml")
	body := []byte(`
server: {}
database:
  dsn: "user:pass@tcp(localhost:3306)/creator"
jwt:
  secret_key: test-secret
model_providers:
  wangcai_openai:
    protocol: openai_compatible
    base_url: http://18.141.196.64:18888/v1
    api_key: "${WANGCAI_OPENAI_API_KEY}"
model_routes:
  image_generation:
    designer:
      gpt_image_2:
        alias: GPT Image 2
        provider: wangcai_openai
        model: gpt-image-2
        enabled: true
        capabilities:
          size_presets: [auto, 1024x1024]
          default_size: auto
          max_batch: 1
          output_formats: [png]
claude:
  plugin_dir: "` + pluginDir + `"
`)
	if err := os.WriteFile(cfgPath, body, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := NewConfig(cfgPath)
	if err == nil || !strings.Contains(err.Error(), "model_routes.image_generation.designer.gpt_image_2.quality_rank must be positive") {
		t.Fatalf("error = %v, want positive quality rank validation error", err)
	}
}

func TestSemanticModelConfigRejectsImageGenerationBusinessSizes(t *testing.T) {
	dir := t.TempDir()
	pluginDir := fakePluginDir(t, dir)
	for name, body := range map[string]string{
		"sizes block": `
server: {}
database:
  dsn: "user:pass@tcp(localhost:3306)/creator"
jwt:
  secret_key: test-secret
model_routes:
  image_generation:
    sizes:
      article_cover: "16:9"
claude:
  plugin_dir: "` + pluginDir + `"
`,
		"cover size": `
server: {}
database:
  dsn: "user:pass@tcp(localhost:3306)/creator"
jwt:
  secret_key: test-secret
model_routes:
  image_generation:
    cover:
      provider: volcengine_ark
      model: doubao-seedream-5-0-pro-260628
      size: "16:9"
claude:
  plugin_dir: "` + pluginDir + `"
`,
	} {
		t.Run(name, func(t *testing.T) {
			cfgPath := filepath.Join(dir, strings.ReplaceAll(name, " ", "-")+".yaml")
			if err := os.WriteFile(cfgPath, []byte(body), 0644); err != nil {
				t.Fatalf("write config: %v", err)
			}
			_, err := NewConfig(cfgPath)
			if err == nil || !strings.Contains(err.Error(), "image size") && !strings.Contains(err.Error(), "image_generation.sizes") {
				t.Fatalf("error = %v, want business image size rejection", err)
			}
		})
	}
}

func fakePluginDir(t *testing.T, dir string) string {
	t.Helper()
	pluginDir := filepath.Join(dir, "plugin")
	if err := os.MkdirAll(filepath.Join(pluginDir, "agents"), 0755); err != nil {
		t.Fatalf("create fake plugin dir: %v", err)
	}
	return pluginDir
}
