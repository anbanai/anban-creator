package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
	t.Setenv("WANGCAI_GPT_IMAGE_2_COST_CNY_PER_IMAGE", "0.18")

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
      model: doubao-seedream-5-0-260128
      size: 9:16:2k
    content:
      provider: volcengine_ark
      model: doubao-seedream-5-0-260128
      size: 9:16:2k
    designer:
      gpt_image_2:
        alias: GPT Image 2
        provider: wangcai_openai
        model: gpt-image-2
        enabled: true
        response_format: url
    sizes:
      article_cover: "16:9"
      article_content: "16:9"
      seednote_cover: "3:4"
      seednote_content: "3:4"
  video_generation:
    provider: volcengine_ark
    timeout: 10m
    default_model: seedance-2.0-mini
    model_catalog:
      - key: seedance-2.0-mini
        display_name: Doubao Seedance 2.0 Mini
        model: doubao-seedance-2-0-mini-260615
        supported_resolutions: [480p, 720p]
        supported_ratios: ["16:9", "9:16"]
        min_duration: 1
        max_duration: 15
        supports_video_input: true
model_prices:
  currency_rates:
    USD: {to_cny: 7.2}
    CNY: {to_cny: 1.0}
  token_models:
    moonshot/kimi-k2.7-code-highspeed:
      currency: USD
      unit: 1000000
      cached_input: 0.38
      input: 1.90
      output: 8.00
  image_generation:
    wangcai_openai/gpt-image-2:
      currency: CNY
      unit: image
      price: "${WANGCAI_GPT_IMAGE_2_COST_CNY_PER_IMAGE}"
billing:
  credits_per_cny: 1000
  tier_multipliers:
    free: 1.30
    pro: 1.15
    enterprise: 1.00
  default_user_multiplier: 1.00
  minimum_charge_credits: 1
image_presets:
  - key: openai-standard
    display_name: GPT Image 2
    provider_route: image_generation.designer.gpt_image_2
    min_tier: pro
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

	if cfg.Writing.Model != "kimi-k2.7-code" || cfg.Writing.BaseURL != "https://api.moonshot.cn/v1" {
		t.Fatalf("derived writing config = %#v", cfg.Writing)
	}
	if cfg.ImageUnderstanding.Model != "kimi-k2.7-code-highspeed" || !cfg.ImageUnderstanding.RequireUsage {
		t.Fatalf("image understanding route = %#v", cfg.ImageUnderstanding)
	}
	if cfg.VideoUnderstanding.Model != "kimi-k2.7-code-highspeed" || !cfg.VideoUnderstanding.RequireNativeVideo {
		t.Fatalf("video understanding route = %#v", cfg.VideoUnderstanding)
	}
	if cfg.ImageAPI.Cover == nil || cfg.ImageAPI.Cover.Provider != "volcengine" || cfg.ImageAPI.Cover.Model != "doubao-seedream-5-0-260128" {
		t.Fatalf("derived image cover config = %#v", cfg.ImageAPI.Cover)
	}
	if cfg.VideoAPI.Key != "ark-test" || len(cfg.VideoAPI.ModelCatalog) != 1 || cfg.VideoAPI.ModelCatalog[0].ModelID != "doubao-seedance-2-0-mini-260615" {
		t.Fatalf("derived video api config = %#v", cfg.VideoAPI)
	}
	if cfg.ImageAPI.Sizes.SeednoteCover != "3:4" {
		t.Fatalf("derived image sizes = %#v", cfg.ImageAPI.Sizes)
	}
	if len(cfg.ImagePresets) != 1 || cfg.ImagePresets[0].Provider != "openai" || cfg.ImagePresets[0].Endpoint != "http://18.141.196.64:18888/v1" || cfg.ImagePresets[0].APIKey != "wangcai-test" {
		t.Fatalf("derived image preset route = %#v", cfg.ImagePresets)
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

func TestTokenModelCostUsesRealCostAndTierMultiplier(t *testing.T) {
	cfg := &Config{
		ModelPrices: ModelPricesConfig{
			CurrencyRates: map[string]CurrencyRate{
				"USD": {ToCNY: 7.2},
			},
			TokenModels: map[string]TokenModelPrice{
				"moonshot/kimi-k2.7-code-highspeed": {
					Currency:    "USD",
					Unit:        1_000_000,
					CachedInput: 0.38,
					Input:       1.90,
					Output:      8.00,
				},
			},
		},
		Billing: BillingConfig{
			CreditsPerCNY:         1000,
			TierMultipliers:       map[string]float64{"free": 1.30, "pro": 1.15, "enterprise": 1.00},
			DefaultUserMultiplier: 1.0,
			MinimumChargeCredits:  1,
		},
	}

	cost, err := cfg.CalculateTokenModelCredits("moonshot", "kimi-k2.7-code-highspeed", TokenUsage{
		InputTokens:       10_000,
		CachedInputTokens: 2_000,
		OutputTokens:      1_000,
		TotalTokens:       11_000,
	}, "free", 1.0)
	if err != nil {
		t.Fatalf("CalculateTokenModelCredits() error = %v", err)
	}

	if cost.BaseCredits != 173 {
		t.Fatalf("base credits = %d, want 173", cost.BaseCredits)
	}
	if cost.FinalCredits != 225 {
		t.Fatalf("final credits = %d, want ceil(173*1.30)=225", cost.FinalCredits)
	}
	if cost.PriceSnapshot.Provider != "moonshot" || cost.PriceSnapshot.Model != "kimi-k2.7-code-highspeed" {
		t.Fatalf("price snapshot = %#v", cost.PriceSnapshot)
	}
}
