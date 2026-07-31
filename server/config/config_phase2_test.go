package config

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
)

func TestDesignerRoutesDeriveSelectablePresetsInStableQualityOrder(t *testing.T) {
	dir := t.TempDir()
	pluginDir := fakePluginDir(t, dir)
	path := filepath.Join(dir, "config.yaml")
	body := `
database:
  dsn: test
jwt:
  secret_key: test-secret
model_providers:
  images:
    protocol: openai_compatible
    base_url: https://images.example.com/v1
    api_key: secret
model_routes:
  image_generation:
    designer:
      lower:
        selection_key: stable-b
        min_tier: free
        alias: Lower
        description: Routine illustrations
        sort_order: 10
        billing_sku: image.seedream.designer
        provider: images
        model: lower
        enabled: true
        quality_rank: 100
        capabilities: &caps
          size_presets: ["1:1"]
          default_size: "1:1"
          max_batch: 1
          max_reference_images: 0
          output_formats: [png]
      higher_z:
        selection_key: stable-z
        min_tier: enterprise
        alias: Higher Z
        billing_sku: image.gpt-image-2.designer
        provider: images
        model: higher-z
        enabled: true
        quality_rank: 200
        capabilities: *caps
      higher_a:
        selection_key: stable-a
        min_tier: pro
        alias: Higher A
        description: Detailed compositions
        sort_order: 20
        billing_sku: image.gpt-image-2.designer
        provider: images
        model: higher-a
        enabled: true
        quality_rank: 200
        capabilities: *caps
      disabled:
        enabled: false
claude:
  executor: docker
  execution_token_secret: 0123456789abcdef0123456789abcdef
  runtime_images:
    article: creator-agent-article:latest
    seednote: creator-agent-seednote:latest
    montage: creator-agent-montage:latest
  plugin_dir: "` + pluginDir + `"
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := NewConfig(path)
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	if len(cfg.ImagePresets) != 3 {
		t.Fatalf("derived presets = %#v", cfg.ImagePresets)
	}
	for index, want := range []string{"stable-b", "stable-a", "stable-z"} {
		if cfg.ImagePresets[index].Key != want {
			t.Fatalf("preset order = %#v, want index %d = %q", cfg.ImagePresets, index, want)
		}
	}
	wantDesignerOrder := []string{"lower", "higher_a", "higher_z"}
	gotDesignerOrder := cfg.ImageAPI.DesignerOrder()
	if !slices.Equal(gotDesignerOrder, wantDesignerOrder) {
		t.Fatalf("designer order = %#v, want %#v", gotDesignerOrder, wantDesignerOrder)
	}
	if got := cfg.ImagePresets[1]; got.DisplayName != "Higher A" || got.Description != "Detailed compositions" || got.SortOrder != 20 || got.BillingSKU != "image.gpt-image-2.designer" || got.MinTier != "pro" || got.ProviderRoute != "image_generation.designer.higher_a" {
		t.Fatalf("derived preset = %#v", got)
	}
}

func TestConfigRejectsLegacyTopLevelImagePresets(t *testing.T) {
	_, err := loadClaudeConfigYAML(t, validClaudeConfigYAML+"image_presets: []\n")
	if err == nil || !strings.Contains(err.Error(), "unknown top-level config key image_presets") {
		t.Fatalf("NewConfig error = %v, want legacy image_presets rejection", err)
	}
}

func TestClaudeExecutionProfileEnvDefaultsMergeBeforeProfileOverrides(t *testing.T) {
	body := strings.Replace(validClaudeConfigYAML,
		"claude:\n",
		"claude:\n  execution_profile_env_defaults:\n    CLAUDE_CODE_EFFORT_LEVEL: high\n    CLAUDE_CODE_MAX_OUTPUT_TOKENS: \"131072\"\n    ENABLE_TOOL_SEARCH: \"true\"\n",
		1,
	)
	body = strings.Replace(body, "        ENABLE_TOOL_SEARCH: \"false\"\n", "        CLAUDE_CODE_EFFORT_LEVEL: medium\n", 1)
	cfg, err := loadClaudeConfigYAML(t, body)
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	profile := cfg.Claude.ExecutionProfiles["quality"]
	if profile.Envs["CLAUDE_CODE_EFFORT_LEVEL"] != "medium" || profile.Envs["CLAUDE_CODE_MAX_OUTPUT_TOKENS"] != "131072" || profile.Envs["ENABLE_TOOL_SEARCH"] != "true" {
		t.Fatalf("resolved profile envs = %#v", profile.Envs)
	}
	if cfg.Claude.ExecutionProfileEnvDefaults["CLAUDE_CODE_EFFORT_LEVEL"] != "high" {
		t.Fatalf("defaults mutated = %#v", cfg.Claude.ExecutionProfileEnvDefaults)
	}
}

func TestClaudeExecutionProfileEnvDefaultsRejectUnknownEnv(t *testing.T) {
	body := strings.Replace(validClaudeConfigYAML, "claude:\n", "claude:\n  execution_profile_env_defaults:\n    PATH: /tmp\n", 1)
	_, err := loadClaudeConfigYAML(t, body)
	if err == nil || !strings.Contains(err.Error(), "claude.execution_profile_env_defaults.PATH") {
		t.Fatalf("NewConfig error = %v, want strict defaults env error", err)
	}
}

func TestClaudeExecutionProfileEnvDefaultsRejectProviderAndModelFields(t *testing.T) {
	for _, key := range []string{
		model.ClaudeEnvBaseURL,
		model.ClaudeEnvAuthToken,
		model.ClaudeEnvModel,
		"ANTHROPIC_DEFAULT_OPUS_MODEL",
		"ANTHROPIC_DEFAULT_FABLE_MODEL",
		"ANTHROPIC_DEFAULT_SONNET_MODEL",
		"ANTHROPIC_DEFAULT_HAIKU_MODEL",
		"CLAUDE_CODE_SUBAGENT_MODEL",
	} {
		t.Run(key, func(t *testing.T) {
			body := strings.Replace(validClaudeConfigYAML, "claude:\n", "claude:\n  execution_profile_env_defaults:\n    "+key+": shared-value\n", 1)
			_, err := loadClaudeConfigYAML(t, body)
			if err == nil || !strings.Contains(err.Error(), key) || !strings.Contains(err.Error(), "must be configured per profile") {
				t.Fatalf("NewConfig error = %v, want profile-owned field rejection", err)
			}
		})
	}
}

func TestClaudeExecutionProfileEnvDefaultsValidateConfiguredValues(t *testing.T) {
	body := strings.Replace(validClaudeConfigYAML, "claude:\n", "claude:\n  execution_profile_env_defaults:\n    CLAUDE_CODE_EFFORT_LEVEL: turbo\n", 1)
	_, err := loadClaudeConfigYAML(t, body)
	if err == nil || !strings.Contains(err.Error(), "claude.execution_profile_env_defaults") || !strings.Contains(err.Error(), "CLAUDE_CODE_EFFORT_LEVEL") {
		t.Fatalf("NewConfig error = %v, want partial defaults validation error", err)
	}
}

func TestDesignerRouteAllowsDisabledMinimalEntry(t *testing.T) {
	body := strings.Replace(validClaudeConfigYAML, "claude:\n", `model_routes:
  image_generation:
    designer:
      future_provider:
        enabled: false
claude:
`, 1)
	cfg, err := loadClaudeConfigYAML(t, body)
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	if len(cfg.ImagePresets) != 0 {
		t.Fatalf("disabled designer route derived presets = %#v", cfg.ImagePresets)
	}
}

func TestDesignerRouteRejectsUnknownField(t *testing.T) {
	body := strings.Replace(validClaudeConfigYAML, "claude:\n", `model_routes:
  image_generation:
    designer:
      future_provider:
        enabled: false
        selection_keey: typo
claude:
`, 1)
	_, err := loadClaudeConfigYAML(t, body)
	if err == nil || !strings.Contains(err.Error(), "selection_keey") {
		t.Fatalf("NewConfig error = %v, want strict designer route field error", err)
	}
}

func TestDesignerRouteRejectsYAMLMergeDefaults(t *testing.T) {
	body := strings.Replace(validClaudeConfigYAML, "claude:\n", `model_routes:
  image_generation:
    designer:
      base: &route
        enabled: false
      inherited:
        <<: *route
claude:
`, 1)
	_, err := loadClaudeConfigYAML(t, body)
	if err == nil || !strings.Contains(err.Error(), "image generation route.<<") {
		t.Fatalf("NewConfig error = %v, want route inheritance rejection", err)
	}
}

func TestEnabledDesignerRouteRequiresSelectableCatalogFields(t *testing.T) {
	base := `
database:
  dsn: test
jwt:
  secret_key: test-secret
model_providers:
  images:
    protocol: openai_compatible
    base_url: https://images.example.com/v1
    api_key: secret
model_routes:
  image_generation:
    designer:
      primary:
        selection_key: stable-image
        min_tier: pro
        alias: Stable Image
        billing_sku: image.seedream.designer
        provider: images
        model: image-v1
        enabled: true
        quality_rank: 100
        capabilities:
          size_presets: ["1:1"]
          default_size: "1:1"
          max_batch: 1
          max_reference_images: 0
          output_formats: [png]
`
	tests := []struct {
		name    string
		old     string
		replace string
		want    string
	}{
		{name: "provider required", old: "provider: images", replace: "provider: \"\"", want: "provider"},
		{name: "model required", old: "model: image-v1", replace: "model: \"\"", want: "model"},
		{name: "selection key required", old: "        selection_key: stable-image\n", replace: "", want: "selection_key"},
		{name: "selection key reserved", old: "selection_key: stable-image", replace: "selection_key: custom", want: "reserved"},
		{name: "tier exact", old: "min_tier: pro", replace: "min_tier: premium", want: "min_tier"},
		{name: "alias required", old: "alias: Stable Image", replace: "alias: \"\"", want: "alias"},
		{name: "billing SKU required", old: "        billing_sku: image.seedream.designer\n", replace: "", want: "billing_sku"},
		{name: "quality rank positive", old: "quality_rank: 100", replace: "quality_rank: 0", want: "quality_rank"},
		{name: "capabilities valid", old: "max_batch: 1", replace: "max_batch: 0", want: "capabilities"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadClaudeConfigYAML(t, strings.Replace(base, tt.old, tt.replace, 1))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("NewConfig error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestEnabledDesignerRouteRequiresConfiguredProvider(t *testing.T) {
	body := `
database:
  dsn: test
jwt:
  secret_key: test-secret
model_routes:
  image_generation:
    designer:
      primary:
        selection_key: stable-image
        min_tier: free
        alias: Stable Image
        provider: images
        model: image-v1
        enabled: true
        quality_rank: 100
        capabilities:
          size_presets: ["1:1"]
          default_size: "1:1"
          max_batch: 1
          max_reference_images: 0
          output_formats: [png]
`
	_, err := loadClaudeConfigYAML(t, body)
	if err == nil || !strings.Contains(err.Error(), `provider "images" is not configured in model_providers`) {
		t.Fatalf("NewConfig error = %v, want missing provider rejection", err)
	}
}

func TestEnabledDesignerRoutesRequireUniqueSelectionKeys(t *testing.T) {
	body := `
database:
  dsn: test
jwt:
  secret_key: test-secret
model_providers:
  images:
    protocol: openai_compatible
    base_url: https://images.example.com/v1
    api_key: secret
model_routes:
  image_generation:
    designer:
      first:
        selection_key: same-key
        min_tier: free
        alias: First
        billing_sku: image.seedream.designer
        provider: images
        model: image-v1
        enabled: true
        quality_rank: 100
        capabilities:
          size_presets: ["1:1"]
          default_size: "1:1"
          max_batch: 1
          max_reference_images: 0
          output_formats: [png]
      second:
        selection_key: same-key
        min_tier: free
        alias: Second
        billing_sku: image.gpt-image-2.designer
        provider: images
        model: image-v2
        enabled: true
        quality_rank: 100
        capabilities:
          size_presets: ["1:1"]
          default_size: "1:1"
          max_batch: 1
          max_reference_images: 0
          output_formats: [png]
`
	_, err := loadClaudeConfigYAML(t, body)
	if err == nil || !strings.Contains(err.Error(), "duplicate key") {
		t.Fatalf("NewConfig error = %v, want duplicate selection key", err)
	}
}

func TestConfigExampleLoadsAsCompleteConfiguration(t *testing.T) {
	raw, err := os.ReadFile("../config.example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for _, match := range regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)`).FindAllStringSubmatch(string(raw), -1) {
		t.Setenv(match[1], "")
	}
	for name, value := range map[string]string{
		"ANBAN_DATABASE_DSN":                 "root:test@tcp(localhost:3306)/anban_creator?parseTime=true",
		"ANBAN_BILLING_ADMIN_API_KEY":        "test-billing-admin-key",
		"ANBAN_AGENT_EXECUTOR":               "docker",
		"ANBAN_AGENT_EXECUTION_TOKEN_SECRET": "0123456789abcdef0123456789abcdef",
		"ANBAN_AGENT_IMAGE_ARTICLE":          "creator-agent-article:latest",
		"ANBAN_AGENT_IMAGE_SEEDNOTE":         "creator-agent-seednote:latest",
		"ANBAN_AGENT_IMAGE_MONTAGE":          "creator-agent-montage:latest",
		"ANBAN_JWT_SECRET_KEY":               "0123456789abcdef0123456789abcdef",
		"ANBAN_OSS_ENDPOINT":                 "oss-cn-test.aliyuncs.com",
		"ANBAN_OSS_ACCESS_KEY_ID":            "test-access-key-id",
		"ANBAN_OSS_ACCESS_KEY_SECRET":        "test-access-key-secret",
		"ANBAN_DEEPSEEK_ANTHROPIC_BASE_URL":  "https://deepseek.example.com/anthropic",
		"ANBAN_DEEPSEEK_API_KEY":             "test-deepseek-api-key",
		"ANBAN_MOONSHOT_ANTHROPIC_BASE_URL":  "https://api.moonshot.cn/anthropic",
		"ANBAN_MOONSHOT_API_KEY":             "test-moonshot-agent-api-key",
		"ANBAN_ZHIPU_ANTHROPIC_BASE_URL":     "https://open.bigmodel.cn/api/anthropic",
		"ANBAN_ZHIPU_API_KEY":                "test-zhipu-api-key",
		"MOONSHOT_API_KEY":                   "test-moonshot-api-key",
		"VOLCENGINE_ARK_API_KEY":             "test-volcengine-api-key",
		"WANGCAI_OPENAI_API_KEY":             "test-openai-api-key",
	} {
		t.Setenv(name, value)
	}
	billingDir, err := filepath.Abs("../billing")
	if err != nil {
		t.Fatal(err)
	}
	body := strings.Replace(string(raw), `config_dir: "./billing"`, `config_dir: "`+billingDir+`"`, 1)
	body = strings.Replace(body, `plugin_dir: "/anbanai"`, `plugin_dir: "`+fakePluginDir(t, t.TempDir())+`"`, 1)
	path := filepath.Join(t.TempDir(), "config.example.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := NewConfig(path)
	if err != nil {
		t.Fatalf("NewConfig(config.example.yaml): %v", err)
	}
	if len(cfg.ImagePresets) != 2 || len(cfg.Claude.ExecutionProfiles) != 3 {
		t.Fatalf("derived catalog sizes: image_presets=%d execution_profiles=%d", len(cfg.ImagePresets), len(cfg.Claude.ExecutionProfiles))
	}
	effective := cfg.Claude.ExecutionProfiles["effective"]
	effective.Envs["ENABLE_TOOL_SEARCH"] = "mutated"
	if cfg.Claude.ExecutionProfiles["balanced"].Envs["ENABLE_TOOL_SEARCH"] != "true" || cfg.Claude.ExecutionProfileEnvDefaults["ENABLE_TOOL_SEARCH"] != "true" {
		t.Fatalf("execution profile env maps share storage: defaults=%#v balanced=%#v", cfg.Claude.ExecutionProfileEnvDefaults, cfg.Claude.ExecutionProfiles["balanced"].Envs)
	}
}
