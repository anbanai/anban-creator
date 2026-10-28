package config

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestConfigRejectsLegacyTopLevelImagePresets(t *testing.T) {
	_, err := loadClaudeConfigYAML(t, validClaudeConfigYAML+"image_presets: []\n")
	if err == nil || !strings.Contains(err.Error(), "unknown top-level config key image_presets") {
		t.Fatalf("NewConfig error = %v, want legacy image_presets rejection", err)
	}
}

func TestArticleResourceModeDefaultsToLegacyAndValidatesConfiguredMode(t *testing.T) {
	cfg, err := loadClaudeConfigYAML(t, validClaudeConfigYAML)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ArticleResources.LifecycleMode != "legacy" {
		t.Fatalf("article_resources.lifecycle_mode = %q, want legacy", cfg.ArticleResources.LifecycleMode)
	}

	for _, mode := range []string{"compatibility", "native"} {
		body := strings.Replace(validClaudeConfigYAML, "database:\n", "article_resources:\n  lifecycle_mode: "+mode+"\ndatabase:\n", 1)
		cfg, err := loadClaudeConfigYAML(t, body)
		if err != nil {
			t.Fatalf("load mode %q: %v", mode, err)
		}
		if cfg.ArticleResources.LifecycleMode != mode {
			t.Fatalf("configured lifecycle mode = %q, want %q", cfg.ArticleResources.LifecycleMode, mode)
		}
	}

	body := strings.Replace(validClaudeConfigYAML, "database:\n", "article_resources:\n  lifecycle_mode: preview\ndatabase:\n", 1)
	if _, err := loadClaudeConfigYAML(t, body); err == nil || !strings.Contains(err.Error(), "article_resources.lifecycle_mode") {
		t.Fatalf("invalid lifecycle mode error = %v, want article_resources.lifecycle_mode", err)
	}
}

func TestClaudeConfigRejectsExecutionProfileEnvDefaults(t *testing.T) {
	body := strings.Replace(
		validClaudeConfigYAML,
		"claude:\n",
		"claude:\n  execution_profile_env_defaults:\n    ENABLE_TOOL_SEARCH: \"true\"\n",
		1,
	)
	_, err := loadClaudeConfigYAML(t, body)
	if err == nil || !strings.Contains(err.Error(), `unknown claude config field "execution_profile_env_defaults"`) {
		t.Fatalf("NewConfig error = %v, want legacy execution_profile_env_defaults rejection", err)
	}
}

func TestClaudeConfigRejectsCatalogDigestRuntimeSetting(t *testing.T) {
	body := strings.Replace(validClaudeConfigYAML, "claude:\n", "claude:\n  agent_pack_catalog_digest: \""+strings.Repeat("a", 64)+"\"\n", 1)
	_, err := loadClaudeConfigYAML(t, body)
	if err == nil || !strings.Contains(err.Error(), `unknown claude config field "agent_pack_catalog_digest"`) {
		t.Fatalf("NewConfig() error = %v, want removed runtime setting rejection", err)
	}
}

func TestRateLimitConfigDefaults(t *testing.T) {
	cfg, err := loadClaudeConfigYAML(t, validClaudeConfigYAML)
	if err != nil {
		t.Fatal(err)
	}
	want := RateLimitConfig{
		API:      RateLimitTierConfig{Requests: 300, Window: time.Minute},
		Auth:     RateLimitTierConfig{Requests: 20, Window: time.Minute},
		SendCode: RateLimitTierConfig{Requests: 5, Window: 10 * time.Minute},
		Agent:    RateLimitTierConfig{Requests: 300, Window: time.Minute},
		Admin:    RateLimitTierConfig{Requests: 10, Window: time.Minute},
	}
	assertRateLimitConfig(t, cfg.RateLimit, want)
}

func TestRateLimitConfigLoadsYAMLAndEnvironmentValues(t *testing.T) {
	t.Setenv("ANBAN_TEST_RATE_LIMIT_API_REQUESTS", "42")
	body := strings.Replace(validClaudeConfigYAML, "claude:\n", `rate_limit:
  api:
    requests: ${ANBAN_TEST_RATE_LIMIT_API_REQUESTS:-41}
    window: 2m
  auth:
    requests: 25
    window: 30s
  send_code:
    requests: 7
    window: 15m
  agent:
    requests: 450
    window: 45s
  admin:
    requests: 15
    window: 2m
claude:
`, 1)
	cfg, err := loadClaudeConfigYAML(t, body)
	if err != nil {
		t.Fatal(err)
	}
	want := RateLimitConfig{
		API:      RateLimitTierConfig{Requests: 42, Window: 2 * time.Minute},
		Auth:     RateLimitTierConfig{Requests: 25, Window: 30 * time.Second},
		SendCode: RateLimitTierConfig{Requests: 7, Window: 15 * time.Minute},
		Agent:    RateLimitTierConfig{Requests: 450, Window: 45 * time.Second},
		Admin:    RateLimitTierConfig{Requests: 15, Window: 2 * time.Minute},
	}
	assertRateLimitConfig(t, cfg.RateLimit, want)
}

func assertRateLimitConfig(t *testing.T, got, want RateLimitConfig) {
	t.Helper()
	for name, gotTier := range map[string]RateLimitTierConfig{
		"api": got.API, "auth": got.Auth, "send_code": got.SendCode, "agent": got.Agent, "admin": got.Admin,
	} {
		var wantTier RateLimitTierConfig
		switch name {
		case "api":
			wantTier = want.API
		case "auth":
			wantTier = want.Auth
		case "send_code":
			wantTier = want.SendCode
		case "agent":
			wantTier = want.Agent
		case "admin":
			wantTier = want.Admin
		}
		if gotTier.Requests != wantTier.Requests || gotTier.Window != wantTier.Window {
			t.Errorf("RateLimit.%s = requests %d/window %s, want requests %d/window %s", name, gotTier.Requests, gotTier.Window, wantTier.Requests, wantTier.Window)
		}
	}
}

func TestRateLimitConfigRejectsNonPositiveValues(t *testing.T) {
	for _, tc := range []struct {
		name  string
		field string
		value string
	}{
		{name: "zero requests", field: "requests", value: "0"},
		{name: "negative requests", field: "requests", value: "-1"},
		{name: "zero window", field: "window", value: "0s"},
		{name: "negative window", field: "window", value: "-1s"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := strings.Replace(validClaudeConfigYAML, "claude:\n", "rate_limit:\n  api:\n    "+tc.field+": "+tc.value+"\nclaude:\n", 1)
			if _, err := loadClaudeConfigYAML(t, body); err == nil || !strings.Contains(err.Error(), "rate_limit.api") {
				t.Fatalf("NewConfig error = %v, want rate_limit.api validation error", err)
			}
		})
	}
}

func TestConfigExampleLoadsAsCompleteConfiguration(t *testing.T) {
	raw, err := os.ReadFile("../config.example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "\n  execution_profile_env_defaults:") {
		t.Fatal("config.example.yaml contains removed claude.execution_profile_env_defaults")
	}
	for _, match := range regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)`).FindAllStringSubmatch(string(raw), -1) {
		t.Setenv(match[1], "")
	}
	for name, value := range map[string]string{
		"ANBAN_DATABASE_DSN":                      "root:test@tcp(localhost:3306)/anban_creator?parseTime=true",
		"ANBAN_BILLING_ADMIN_API_KEY":             "test-billing-admin-key",
		"ANBAN_AGENT_EXECUTOR":                    "docker",
		"ANBAN_AGENT_EXECUTION_TOKEN_SECRET":      "0123456789abcdef0123456789abcdef",
		"ANBAN_DSH_EXECUTOR":                      "kubernetes",
		"ANBAN_DSH_IMAGE":                         "registry.example.com/creator-dsh-runtime@sha256:" + strings.Repeat("a", 64),
		"ANBAN_AGENT_IMAGE_WECHAT_ARTICLE":        "creator-agent-wechat-article:latest",
		"ANBAN_AGENT_IMAGE_WECHAT_PICTURE":        "creator-agent-wechat-picture:latest",
		"ANBAN_AGENT_IMAGE_SEEDNOTE":              "creator-agent-seednote:latest",
		"ANBAN_AGENT_IMAGE_MONTAGE":               "creator-agent-montage:latest",
		"ANBAN_AGENT_IMAGE_VIRAL_IP_VIDEO_EDITOR": "creator-agent-viral-ip-video-editor:latest",
		"ANBAN_AGENT_IMAGE_VIDEO_EDIT":            "creator-agent-video-edit:latest",
		"ANBAN_AGENT_IMAGE_MOMENTS":               "creator-agent-moments:latest",
		"ANBAN_AGENT_IMAGE_ECOMMERCE":             "creator-agent-ecommerce:latest",
		"ANBAN_AGENT_IMAGE_PROFILE_ANALYSIS":      "creator-agent-profile-analysis:latest",
		"ANBAN_AGENT_IMAGE_FEEDBACK_ANALYSIS":     "creator-agent-feedback-analysis:latest",
		"ANBAN_AGENT_IMAGE_WHITEBOARD_ANIMATION":  "creator-agent-whiteboard-animation:latest",
		"ANBAN_AGENT_IMAGE_HYPIT":                 "creator-agent-hypit:latest",
		"ANBAN_JWT_SECRET_KEY":                    "0123456789abcdef0123456789abcdef",
		"ANBAN_OSS_ENDPOINT":                      "oss-cn-test.aliyuncs.com",
		"ANBAN_OSS_ACCESS_KEY_ID":                 "test-access-key-id",
		"ANBAN_OSS_ACCESS_KEY_SECRET":             "test-access-key-secret",
		"ANBAN_DEEPSEEK_ANTHROPIC_BASE_URL":       "https://deepseek.example.com/anthropic",
		"ANBAN_DEEPSEEK_API_KEY":                  "test-deepseek-api-key",
		"ANBAN_MOONSHOT_ANTHROPIC_BASE_URL":       "https://api.moonshot.cn/anthropic",
		"ANBAN_MOONSHOT_API_KEY":                  "test-moonshot-agent-api-key",
		"ANBAN_ZHIPU_ANTHROPIC_BASE_URL":          "https://open.bigmodel.cn/api/anthropic",
		"ANBAN_ZHIPU_API_KEY":                     "test-zhipu-api-key",
		"MOONSHOT_API_KEY":                        "test-moonshot-api-key",
		"VOLCENGINE_ARK_API_KEY":                  "test-volcengine-api-key",
		"WANGCAI_OPENAI_BASE_URL":                 "https://images.example.com/v1",
		"WANGCAI_OPENAI_API_KEY":                  "test-openai-api-key",
		"ANBAN_IMAGE_MODEL_PROFESSIONAL":          "gpt-image-2.5-sunburst",
		"VIDEO_EDIT_ELEVENLABS_API_KEY":           "test-video-edit-scribe-key",
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
	if len(cfg.ModelRoutes.ImageGeneration.Capabilities) != 2 || len(cfg.Claude.ExecutionProfiles) != 3 {
		t.Fatalf("catalog sizes: image_capabilities=%d profiles=%d", len(cfg.ModelRoutes.ImageGeneration.Capabilities), len(cfg.Claude.ExecutionProfiles))
	}
	if cfg.Dsh.Executor != "kubernetes" || cfg.Dsh.Image != "registry.example.com/creator-dsh-runtime@sha256:"+strings.Repeat("a", 64) {
		t.Fatalf("DSH Kubernetes image config = %q/%q", cfg.Dsh.Executor, cfg.Dsh.Image)
	}
	if got := cfg.Claude.RuntimeImages["video-edit"]; got != "creator-agent-video-edit:latest" {
		t.Fatalf("video-edit runtime image = %q, want creator-agent-video-edit:latest", got)
	}
	if !cfg.VideoEdit.DispatchReady() || cfg.VideoEdit.ElevenLabsAPIKey != "test-video-edit-scribe-key" {
		t.Fatalf("video-edit dispatch configuration did not load server-owned Scribe credential")
	}
	professional := cfg.ModelRoutes.ImageGeneration.Capabilities["professional"]
	if cfg.ModelRoutes.ImageGeneration.DefaultCapability != "standard" || cfg.ModelRoutes.ImageGeneration.Capabilities["standard"].MinTier != "free" {
		t.Fatalf("default image capability = %q with min_tier=%q, want capability standard with min_tier free", cfg.ModelRoutes.ImageGeneration.DefaultCapability, cfg.ModelRoutes.ImageGeneration.Capabilities["standard"].MinTier)
	}
	if professional.Provider+"/"+professional.Model != "wangcai_openai/gpt-image-2.5-sunburst" {
		t.Fatalf("professional image model = %q/%q, want model present in billing costs", professional.Provider, professional.Model)
	}
	if professional.ResponseFormat != "b64_json" {
		t.Fatalf("professional image response_format = %q, want b64_json", professional.ResponseFormat)
	}
	if professional.Timeout != 8*time.Minute {
		t.Fatalf("professional image timeout = %s, want 8m", professional.Timeout)
	}
	fixedImageGenerationSize := regexp.MustCompile(`^[1-9][0-9]*:[1-9][0-9]*:(1K|2K|4K)$`)
	for key, capability := range cfg.ModelRoutes.ImageGeneration.Capabilities {
		if !capability.Enabled {
			continue
		}
		modelID := strings.TrimSpace(capability.Provider) + "/" + strings.TrimSpace(capability.Model)
		if _, ok := cfg.BillingBundle.Costs.Models[modelID]; !ok {
			t.Fatalf("enabled image capability %q uses %q, which is missing from billing costs.models", key, modelID)
		}
		if capability.GenerationFeatures.MaxBatch != 1 {
			t.Fatalf("enabled image capability %q max_batch = %d, want 1 for fixed per-image billing", key, capability.GenerationFeatures.MaxBatch)
		}
		for _, preset := range capability.GenerationFeatures.SizePresets {
			if !fixedImageGenerationSize.MatchString(preset) {
				t.Fatalf("enabled image capability %q exposes non-fixed generation size %q", key, preset)
			}
		}
	}
	expectedRuntimeControls := map[string]map[string]string{
		"effective": {
			"CLAUDE_CODE_EFFORT_LEVEL":                 "medium",
			"CLAUDE_CODE_ALWAYS_ENABLE_EFFORT":         "false",
			"CLAUDE_CODE_MAX_CONTEXT_TOKENS":           "1048576",
			"CLAUDE_CODE_MAX_OUTPUT_TOKENS":            "393216",
			"MAX_THINKING_TOKENS":                      "0",
			"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1",
			"CLAUDE_CODE_DISABLE_AUTO_MEMORY":          "0",
			"CLAUDE_CODE_DISABLE_ADAPTIVE_THINKING":    "false",
			"CLAUDE_CODE_DISABLE_THINKING":             "false",
			"CLAUDE_CODE_AUTO_COMPACT_WINDOW":          "262144",
			"CLAUDE_AUTOCOMPACT_PCT_OVERRIDE":          "80",
			"CLAUDE_CODE_DISABLE_1M_CONTEXT":           "false",
			"ENABLE_TOOL_SEARCH":                       "true",
		},
		"balanced": {
			"CLAUDE_CODE_EFFORT_LEVEL":                 "high",
			"CLAUDE_CODE_ALWAYS_ENABLE_EFFORT":         "false",
			"CLAUDE_CODE_MAX_CONTEXT_TOKENS":           "1048576",
			"CLAUDE_CODE_MAX_OUTPUT_TOKENS":            "131072",
			"MAX_THINKING_TOKENS":                      "0",
			"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1",
			"CLAUDE_CODE_DISABLE_AUTO_MEMORY":          "0",
			"CLAUDE_CODE_DISABLE_ADAPTIVE_THINKING":    "false",
			"CLAUDE_CODE_DISABLE_THINKING":             "false",
			"CLAUDE_CODE_AUTO_COMPACT_WINDOW":          "262144",
			"CLAUDE_AUTOCOMPACT_PCT_OVERRIDE":          "80",
			"CLAUDE_CODE_DISABLE_1M_CONTEXT":           "false",
			"ENABLE_TOOL_SEARCH":                       "true",
		},
		"quality": {
			"CLAUDE_CODE_EFFORT_LEVEL":                 "high",
			"CLAUDE_CODE_ALWAYS_ENABLE_EFFORT":         "true",
			"CLAUDE_CODE_MAX_CONTEXT_TOKENS":           "1048576",
			"CLAUDE_CODE_MAX_OUTPUT_TOKENS":            "131072",
			"MAX_THINKING_TOKENS":                      "0",
			"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1",
			"CLAUDE_CODE_DISABLE_AUTO_MEMORY":          "0",
			"CLAUDE_CODE_DISABLE_ADAPTIVE_THINKING":    "false",
			"CLAUDE_CODE_DISABLE_THINKING":             "false",
			"CLAUDE_CODE_AUTO_COMPACT_WINDOW":          "262144",
			"CLAUDE_AUTOCOMPACT_PCT_OVERRIDE":          "80",
			"CLAUDE_CODE_DISABLE_1M_CONTEXT":           "false",
			"ENABLE_TOOL_SEARCH":                       "true",
		},
	}
	for name, expected := range expectedRuntimeControls {
		for key, want := range expected {
			if got := cfg.Claude.ExecutionProfiles[name].Envs[key]; got != want {
				t.Errorf("%s env %s = %q, want %q", name, key, got, want)
			}
		}
	}
	if got := cfg.Claude.ExecutionProfiles["effective"].ModelUsageAliases; len(got) != 1 || got["deepseek-flash[1m]"] != "deepseek-flash" {
		t.Errorf("effective model_usage_aliases = %#v, want deepseek-flash[1m]: deepseek-flash", got)
	}
	if got := len(cfg.Claude.ExecutionProfiles["balanced"].ModelUsageAliases); got != 0 {
		t.Errorf("balanced model_usage_aliases length = %d, want 0", got)
	}
	if got := cfg.Claude.ExecutionProfiles["quality"].ModelUsageAliases; len(got) != 1 || got["kimi-k3[1m]"] != "kimi-k3" {
		t.Errorf("quality model_usage_aliases = %#v, want only kimi-k3[1m]: kimi-k3", got)
	}
	effective := cfg.Claude.ExecutionProfiles["effective"]
	effective.Envs["ENABLE_TOOL_SEARCH"] = "mutated"
	if got := cfg.Claude.ExecutionProfiles["balanced"].Envs["ENABLE_TOOL_SEARCH"]; got != "true" {
		t.Fatalf("execution profile env maps share storage: balanced ENABLE_TOOL_SEARCH = %q, want true", got)
	}
}
