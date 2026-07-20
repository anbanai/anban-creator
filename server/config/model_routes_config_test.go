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
      pricing_type: openai_image_usage
      currency: USD
      unit: 1000000
      require_usage: true
      text_input: 5.00
      text_cached_input: 1.25
      image_input: 8.00
      image_cached_input: 2.00
      image_output: 30.00
      estimate_table:
        "1024x1024": {low: 0.006, medium: 0.053, high: 0.211}
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
	if cfg.ImageAPI.Cover == nil || cfg.ImageAPI.Cover.Provider != "volcengine" || cfg.ImageAPI.Cover.Model != "doubao-seedream-5-0-pro-260628" {
		t.Fatalf("derived image cover config = %#v", cfg.ImageAPI.Cover)
	}
	if cfg.VideoAPI.Key != "ark-test" || len(cfg.VideoAPI.ModelCatalog) != 1 || cfg.VideoAPI.ModelCatalog[0].ModelID != "doubao-seedance-2-0-mini-260615" {
		t.Fatalf("derived video api config = %#v", cfg.VideoAPI)
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

func TestSemanticModelConfigRequiresUnderstandingRoutePrices(t *testing.T) {
	t.Setenv("MOONSHOT_API_KEY", "moonshot-test")
	dir := t.TempDir()
	pluginDir := fakePluginDir(t, dir)

	for _, tc := range []struct {
		name      string
		route     string
		wantRoute string
		wantPrice string
	}{
		{
			name: "image understanding",
			route: `
  image_understanding:
    provider: moonshot
    model: kimi-k2.7-code
    require_usage: true`,
			wantRoute: "model_routes.image_understanding",
			wantPrice: "model_prices.token_models.moonshot/kimi-k2.7-code",
		},
		{
			name: "video understanding",
			route: `
  video_understanding:
    provider: moonshot
    model: kimi-k2.7-code-highspeed
    require_usage: true`,
			wantRoute: "model_routes.video_understanding",
			wantPrice: "model_prices.token_models.moonshot/kimi-k2.7-code-highspeed",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfgPath := filepath.Join(dir, strings.ReplaceAll(tc.name, " ", "-")+".yaml")
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
model_routes:` + tc.route + `
claude:
  plugin_dir: "` + pluginDir + `"
`)
			if err := os.WriteFile(cfgPath, body, 0644); err != nil {
				t.Fatalf("write config: %v", err)
			}

			_, err := NewConfig(cfgPath)
			if err == nil {
				t.Fatal("NewConfig() succeeded without understanding model price")
			}
			for _, want := range []string{tc.wantRoute, tc.wantPrice} {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("error = %v, want %q", err, want)
				}
			}
		})
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
model_prices:
  image_generation:
    wangcai_openai/gpt-image-2:
      pricing_type: openai_image_usage
      currency: USD
      unit: 1000000
      require_usage: true
      text_input: 5.00
      text_cached_input: 1.25
      image_input: 8.00
      image_cached_input: 2.00
      image_output: 30.00
      estimate_table:
        "1024x1024": {medium: 0.053}
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
model_prices:
  image_generation:
    wangcai_openai/gpt-image-2:
      pricing_type: openai_image_usage
      currency: USD
      unit: 1000000
      require_usage: true
      text_input: 5.00
      text_cached_input: 1.25
      image_input: 8.00
      image_cached_input: 2.00
      image_output: 30.00
      estimate_table:
        "1024x1024": {medium: 0.053}
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

func TestTokenModelCostAppliesUserMultiplier(t *testing.T) {
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
			TierMultipliers:       map[string]float64{"free": 1.30},
			DefaultUserMultiplier: 1.0,
			MinimumChargeCredits:  1,
		},
	}

	cost, err := cfg.CalculateTokenModelCredits("moonshot", "kimi-k2.7-code-highspeed", TokenUsage{
		InputTokens:       10_000,
		CachedInputTokens: 2_000,
		OutputTokens:      1_000,
		TotalTokens:       11_000,
	}, "free", 0.5)
	if err != nil {
		t.Fatalf("CalculateTokenModelCredits() error = %v", err)
	}

	if cost.BaseCredits != 173 {
		t.Fatalf("base credits = %d, want 173", cost.BaseCredits)
	}
	if cost.FinalCredits != 113 {
		t.Fatalf("final credits = %d, want ceil(173*1.30*0.5)=113", cost.FinalCredits)
	}
	if cost.UserMultiplier != 0.5 {
		t.Fatalf("user multiplier = %v, want 0.5", cost.UserMultiplier)
	}
}

func TestDefaultRechargeTiers(t *testing.T) {
	cfg := &Config{}
	cfg.applyDefaults()

	if len(cfg.RechargeTiers) != 3 {
		t.Fatalf("recharge tiers len = %d, want 3", len(cfg.RechargeTiers))
	}
	if got := cfg.RechargeTiers[2]; got.PriceCNY != 100 || got.Credits != 110000 || got.BonusCredits != 10000 || !got.Enabled {
		t.Fatalf("third recharge tier = %#v, want enabled 100 CNY / 110000 credits / 10000 bonus", got)
	}
}

func TestTaskCostDefaultsFillPartialMap(t *testing.T) {
	cfg := &Config{
		Credits: CreditsConfig{
			TaskCosts: map[string]int{"article": 4500},
		},
	}
	cfg.applyDefaults()

	want := map[string]int{
		"article":        4500,
		"seednote":       3600,
		"ecommerce":      3000,
		"videocreator":   2000,
		"videoeditor":    2000,
		"montage":        2000,
		"viral_analysis": 1200,
	}
	for key, value := range want {
		if got := cfg.Credits.TaskCosts[key]; got != value {
			t.Fatalf("task_costs[%s] = %d, want %d in %#v", key, got, value, cfg.Credits.TaskCosts)
		}
	}
}

func TestAgentRuntimeReserveDefaultsDoNotCreateRuntimeReserve(t *testing.T) {
	cfg := &Config{}
	cfg.applyDefaults()

	if len(cfg.Credits.AgentRuntimeReserve) != 0 {
		t.Fatalf("agent_runtime_reserve = %#v, want empty because runtime cost is platform-paid", cfg.Credits.AgentRuntimeReserve)
	}
}

func TestAgentRuntimeReserveDefaultsPreserveLegacyExplicitValues(t *testing.T) {
	cfg := &Config{
		Credits: CreditsConfig{
			AgentRuntimeReserve: map[string]int{"article": 4500},
		},
	}
	cfg.applyDefaults()

	if len(cfg.Credits.AgentRuntimeReserve) != 1 || cfg.Credits.AgentRuntimeReserve["article"] != 4500 {
		t.Fatalf("agent_runtime_reserve = %#v, want only explicit legacy value", cfg.Credits.AgentRuntimeReserve)
	}
}

func TestRechargeTiersParseFromConfig(t *testing.T) {
	dir := t.TempDir()
	pluginDir := fakePluginDir(t, dir)
	cfgPath := filepath.Join(dir, "config.yaml")
	body := []byte(`
database:
  dsn: "user:pass@tcp(localhost:3306)/creator"
jwt:
  secret_key: test-secret
claude:
  plugin_dir: "` + pluginDir + `"
recharge_tiers:
  - key: basic
    label: 基础包
    price_cny: 10
    credits: 10000
    bonus_credits: 0
    enabled: true
  - key: hidden
    label: 隐藏包
    price_cny: 100
    credits: 110000
    bonus_credits: 10000
    enabled: false
`)
	if err := os.WriteFile(cfgPath, body, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := NewConfig(cfgPath)
	if err != nil {
		t.Fatalf("NewConfig() error = %v", err)
	}
	if len(cfg.RechargeTiers) != 2 {
		t.Fatalf("recharge tiers len = %d, want 2", len(cfg.RechargeTiers))
	}
	if tiers := cfg.EnabledRechargeTiers(); len(tiers) != 1 || tiers[0].Key != "basic" {
		t.Fatalf("enabled recharge tiers = %#v, want only basic", tiers)
	}
}

func TestEnabledRechargeTiersFiltersDisabled(t *testing.T) {
	cfg := &Config{RechargeTiers: []RechargeTierConfig{
		{Key: "basic", PriceCNY: 10, Credits: 10000, Enabled: true},
		{Key: "hidden", PriceCNY: 100, Credits: 110000, Enabled: false},
	}}

	tiers := cfg.EnabledRechargeTiers()
	if len(tiers) != 1 || tiers[0].Key != "basic" {
		t.Fatalf("enabled tiers = %#v, want only basic", tiers)
	}
}

func TestImageGenerationEstimateAndUsageCredits(t *testing.T) {
	cfg := &Config{
		ModelPrices: ModelPricesConfig{
			CurrencyRates: map[string]CurrencyRate{
				"USD": {ToCNY: 7.2},
			},
			ImageGeneration: map[string]ImageGenerationPrice{
				"wangcai_openai/gpt-image-2": {
					PricingType:      ImagePricingTypeOpenAIUsage,
					Currency:         "USD",
					Unit:             1_000_000,
					RequireUsage:     true,
					TextInput:        5.00,
					TextCachedInput:  1.25,
					ImageInput:       8.00,
					ImageCachedInput: 2.00,
					ImageOutput:      30.00,
					EstimateTable: map[string]map[string]FlexibleFloat{
						"1024x1024": {"medium": FlexibleFloat(0.053)},
					},
				},
			},
		},
		Billing: BillingConfig{
			CreditsPerCNY:         1000,
			TierMultipliers:       map[string]float64{"pro": 1.15},
			DefaultUserMultiplier: 1,
			MinimumChargeCredits:  1,
		},
	}

	estimate, err := cfg.CalculateImageGenerationEstimateCredits("wangcai_openai", "gpt-image-2", ImageGenerationUsage{
		Size:    "1024x1024",
		Quality: "medium",
		Count:   1,
	}, "pro", 1)
	if err != nil {
		t.Fatalf("CalculateImageGenerationEstimateCredits() error = %v", err)
	}
	if estimate.BaseCredits != 382 || estimate.FinalCredits != 440 || !estimate.Estimated {
		t.Fatalf("estimate = %#v, want base=382 final=440 estimated=true", estimate)
	}

	usage, err := cfg.CalculateImageGenerationUsageCredits("wangcai_openai", "gpt-image-2", ImageGenerationUsage{
		Size:              "1024x1024",
		Quality:           "medium",
		Count:             1,
		TextInputTokens:   20,
		ImageInputTokens:  100,
		ImageOutputTokens: 1767,
		TotalTokens:       1887,
	}, "pro", 1)
	if err != nil {
		t.Fatalf("CalculateImageGenerationUsageCredits() error = %v", err)
	}
	if usage.Estimated {
		t.Fatalf("usage cost should not be estimated: %#v", usage)
	}
	if usage.BaseCredits != 389 || usage.FinalCredits != 448 {
		t.Fatalf("usage credits = base %d final %d, want base 389 final 448", usage.BaseCredits, usage.FinalCredits)
	}
	if usage.PriceSnapshot.PricingType != ImagePricingTypeOpenAIUsage || usage.PriceSnapshot.ImageOutput != 30 {
		t.Fatalf("price snapshot = %#v", usage.PriceSnapshot)
	}
}

func TestImageGenerationUsageCreditsRequireUsageForGPTImage2(t *testing.T) {
	cfg := &Config{
		ModelPrices: ModelPricesConfig{
			CurrencyRates: map[string]CurrencyRate{"USD": {ToCNY: 7.2}},
			ImageGeneration: map[string]ImageGenerationPrice{
				"wangcai_openai/gpt-image-2": {
					PricingType:  ImagePricingTypeOpenAIUsage,
					Currency:     "USD",
					Unit:         1_000_000,
					RequireUsage: true,
					ImageOutput:  30,
				},
			},
		},
		Billing: BillingConfig{CreditsPerCNY: 1000, MinimumChargeCredits: 1},
	}

	_, err := cfg.CalculateImageGenerationUsageCredits("wangcai_openai", "gpt-image-2", ImageGenerationUsage{
		Size: "1024x1024", Quality: "medium", Count: 1,
	}, "free", 1)
	if err == nil || !strings.Contains(err.Error(), "usage is required") {
		t.Fatalf("error = %v, want usage required", err)
	}
}

func TestImageGenerationEstimateCreditsUsesDefaultUSDRate(t *testing.T) {
	cfg := &Config{
		ModelPrices: ModelPricesConfig{
			ImageGeneration: map[string]ImageGenerationPrice{
				"wangcai_openai/gpt-image-2": {
					PricingType:  ImagePricingTypeOpenAIUsage,
					Currency:     "USD",
					Unit:         1_000_000,
					RequireUsage: true,
					EstimateTable: map[string]map[string]FlexibleFloat{
						"1024x1024": {"medium": FlexibleFloat(0.053)},
					},
				},
			},
		},
		Billing: BillingConfig{CreditsPerCNY: 1000, MinimumChargeCredits: 1},
	}
	cfg.applyDefaults()

	estimate, err := cfg.CalculateImageGenerationEstimateCredits("wangcai_openai", "gpt-image-2", ImageGenerationUsage{
		Size: "1024x1024", Quality: "medium", Count: 1,
	}, "free", 1)
	if err != nil {
		t.Fatalf("CalculateImageGenerationEstimateCredits() error = %v", err)
	}
	if estimate.PriceSnapshot.Currency != "USD" || estimate.PriceSnapshot.CurrencyToCNY != 7.2 {
		t.Fatalf("price snapshot = %#v, want USD at 7.2", estimate.PriceSnapshot)
	}
}

func TestSemanticModelConfigRejectsGPTImage2FixedPrice(t *testing.T) {
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
        quality_rank: 200
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
model_prices:
  image_generation:
    wangcai_openai/gpt-image-2:
      currency: CNY
      unit: image
      price: 0.38
claude:
  plugin_dir: "` + pluginDir + `"
`)
	if err := os.WriteFile(cfgPath, body, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := NewConfig(cfgPath)
	if err == nil || !strings.Contains(err.Error(), "gpt-image-2 requires pricing_type openai_image_usage") {
		t.Fatalf("error = %v, want gpt-image-2 fixed price rejection", err)
	}
}
