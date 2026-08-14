package config

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestConfigRejectsLegacyTopLevelImagePresets(t *testing.T) {
	_, err := loadClaudeConfigYAML(t, validClaudeConfigYAML+"image_presets: []\n")
	if err == nil || !strings.Contains(err.Error(), "unknown top-level config key image_presets") {
		t.Fatalf("NewConfig error = %v, want legacy image_presets rejection", err)
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
	if len(cfg.ModelRoutes.ImageGeneration.Capabilities) != 2 || len(cfg.Claude.ExecutionProfiles) != 3 {
		t.Fatalf("catalog sizes: image_capabilities=%d execution_profiles=%d", len(cfg.ModelRoutes.ImageGeneration.Capabilities), len(cfg.Claude.ExecutionProfiles))
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
	for _, name := range []string{"effective", "balanced"} {
		if got := len(cfg.Claude.ExecutionProfiles[name].ModelUsageAliases); got != 0 {
			t.Errorf("%s model_usage_aliases length = %d, want 0", name, got)
		}
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
