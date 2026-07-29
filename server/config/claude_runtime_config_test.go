package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
)

const validClaudeConfigYAML = `
database:
  dsn: test
jwt:
  secret_key: test-secret
claude:
  providers:
    moonshot:
      protocol: anthropic
      base_url: https://api.moonshot.cn/anthropic
      auth_token: profile-secret-token
  execution_profiles:
    maximum_quality:
      provider: moonshot
      description: flagship
      models:
        default: kimi-k3[1m]
        opus: kimi-k3[1m]
        fable: kimi-k3[1m]
        sonnet: kimi-k3[1m]
        haiku: kimi-k3[1m]
      model_usage_aliases:
        kimi-k3: kimi-k3
        kimi-k3[1m]: kimi-k3
      claude:
        max_thinking_tokens: 0
        enable_tool_search: false
  executor: docker
  execution_token_secret: 0123456789abcdef0123456789abcdef
  runtime_images:
    article: creator-agent-article:latest
    seednote: creator-agent-seednote:latest
    montage: creator-agent-montage:latest
`

func validClaudeConfigForTest() ClaudeConfig {
	return ClaudeConfig{
		Providers: map[string]ClaudeProviderConfig{
			"moonshot": {
				Protocol: "anthropic", BaseURL: "https://api.moonshot.cn/anthropic",
				AuthToken: "profile-secret-token",
			},
		},
		ExecutionProfiles: map[string]ClaudeExecutionProfileConfig{
			"maximum_quality": {
				Provider: "moonshot", Description: "flagship",
				Models: ClaudeModelMatrixConfig{
					Default: "kimi-k3[1m]", Opus: "kimi-k3[1m]", Fable: "kimi-k3[1m]",
					Sonnet: "kimi-k3[1m]", Haiku: "kimi-k3[1m]",
				},
				ModelUsageAliases: map[string]string{"kimi-k3": "kimi-k3", "kimi-k3[1m]": "kimi-k3"},
			},
		},
		Executor:             "docker",
		ExecutionTokenSecret: "0123456789abcdef0123456789abcdef",
		RuntimeImages: RuntimeImages{
			model.PlatformArticle:  "creator-agent-article:latest",
			model.PlatformSeednote: "creator-agent-seednote:latest",
			model.PlatformMontage:  "creator-agent-montage:latest",
		},
	}
}

func loadClaudeConfigYAML(t *testing.T, body string) (*Config, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return NewConfig(path)
}

func TestClaudeConfigStartsWithoutLegacySingleProviderRuntime(t *testing.T) {
	cfg, err := loadClaudeConfigYAML(t, validClaudeConfigYAML)
	if err != nil {
		t.Fatalf("profile-only Claude runtime must start without legacy provider config: %v", err)
	}
	if cfg.Claude.Executor != "docker" || cfg.Claude.Providers["moonshot"].Protocol != "anthropic" {
		t.Fatalf("Claude runtime config = %#v", cfg.Claude)
	}
}

func TestClaudeControlsPreserveOmittedFalseAndZero(t *testing.T) {
	cfg, err := loadClaudeConfigYAML(t, validClaudeConfigYAML)
	if err != nil {
		t.Fatal(err)
	}
	controls := cfg.Claude.ExecutionProfiles["maximum_quality"].Claude
	if controls.MaxThinkingTokens == nil || *controls.MaxThinkingTokens != 0 {
		t.Fatalf("max_thinking_tokens = %v, want pointer to zero", controls.MaxThinkingTokens)
	}
	if controls.EnableToolSearch == nil || *controls.EnableToolSearch {
		t.Fatalf("enable_tool_search = %v, want pointer to false", controls.EnableToolSearch)
	}
	if controls.MaxOutputTokens != nil {
		t.Fatalf("max_output_tokens = %v, want nil when omitted", controls.MaxOutputTokens)
	}
}

func TestClaudeConfigRejectsLegacySingleProviderFields(t *testing.T) {
	for _, test := range []struct {
		name  string
		field string
		value string
	}{
		{name: "provider", field: "provider", value: "volcengine_ark"},
		{name: "base URL", field: "base_url", value: "https://ark.example.com"},
		{name: "auth token", field: "auth_token", value: "secret"},
		{name: "models", field: "models", value: "\n    default: old-model"},
		{name: "usage aliases", field: "model_usage_aliases", value: "\n    old-model: old-model"},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := strings.Replace(validClaudeConfigYAML, "claude:\n", "claude:\n  "+test.field+": "+test.value+"\n", 1)
			_, err := loadClaudeConfigYAML(t, body)
			if err == nil || !strings.Contains(err.Error(), "claude."+test.field+" is not supported") {
				t.Fatalf("NewConfig() error = %v", err)
			}
		})
	}
}

func TestClaudeConfigRejectsRemovedEnv(t *testing.T) {
	body := strings.Replace(validClaudeConfigYAML, "claude:\n", "claude:\n  env:\n    CLAUDE_CODE_AUTO_COMPACT_WINDOW: 1000\n", 1)
	_, err := loadClaudeConfigYAML(t, body)
	if err == nil || !strings.Contains(err.Error(), `unknown claude config field "env"`) {
		t.Fatalf("NewConfig() error = %v", err)
	}
}

func TestClaudeProfileConfigRejectsInvalidFields(t *testing.T) {
	tests := []struct {
		name        string
		old         string
		replacement string
		wantPath    string
	}{
		{name: "empty model role", old: "        default: kimi-k3[1m]", replacement: `        default: ""`, wantPath: "claude.execution_profiles.maximum_quality.models.default"},
		{name: "invalid effort", old: "        max_thinking_tokens: 0", replacement: "        effort_level: extreme\n        max_thinking_tokens: 0", wantPath: "claude.execution_profiles.maximum_quality.claude.effort_level"},
		{name: "invalid autocompact percent", old: "        max_thinking_tokens: 0", replacement: "        autocompact_pct_override: 101\n        max_thinking_tokens: 0", wantPath: "claude.execution_profiles.maximum_quality.claude.autocompact_pct_override"},
		{name: "empty subagent model", old: "        max_thinking_tokens: 0", replacement: "        subagent_model: \"\"\n        max_thinking_tokens: 0", wantPath: "claude.execution_profiles.maximum_quality.claude.subagent_model"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body := strings.Replace(validClaudeConfigYAML, test.old, test.replacement, 1)
			_, err := loadClaudeConfigYAML(t, body)
			if err == nil || !strings.Contains(err.Error(), test.wantPath) {
				t.Fatalf("NewConfig() error = %v, want field path %q", err, test.wantPath)
			}
		})
	}
}

func TestClaudeProfileConfigRejectsUnknownNestedFields(t *testing.T) {
	tests := []struct {
		name        string
		old         string
		replacement string
		wantPath    string
	}{
		{name: "provider", old: "      protocol: anthropic", replacement: "      region: cn\n      protocol: anthropic", wantPath: "claude.providers.moonshot.region"},
		{name: "profile", old: "      provider: moonshot", replacement: "      product_tier: flagship\n      provider: moonshot", wantPath: "claude.execution_profiles.maximum_quality.product_tier"},
		{name: "models", old: "        default: kimi-k3[1m]", replacement: "        legacy: kimi-k3[1m]\n        default: kimi-k3[1m]", wantPath: "claude.execution_profiles.maximum_quality.models.legacy"},
		{name: "controls", old: "        max_thinking_tokens: 0", replacement: "        legacy_thinking: true\n        max_thinking_tokens: 0", wantPath: "claude.execution_profiles.maximum_quality.claude.legacy_thinking"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body := strings.Replace(validClaudeConfigYAML, test.old, test.replacement, 1)
			_, err := loadClaudeConfigYAML(t, body)
			if err == nil || !strings.Contains(err.Error(), test.wantPath) {
				t.Fatalf("NewConfig() error = %v, want field path %q", err, test.wantPath)
			}
		})
	}
}

func TestClaudeConfigRedactsExecutionProfileSecrets(t *testing.T) {
	cfg, err := loadClaudeConfigYAML(t, validClaudeConfigYAML)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(cfg.Claude)
	if err != nil {
		t.Fatal(err)
	}
	for _, output := range []string{fmt.Sprintf("%v", cfg.Claude), fmt.Sprintf("%+v", cfg.Claude), fmt.Sprintf("%#v", cfg.Claude), string(raw)} {
		if strings.Contains(output, "profile-secret-token") {
			t.Fatalf("Claude config leaked execution profile token: %s", output)
		}
	}
}

func TestProductionClaudeConfigUsesExecutionProfilesOnly(t *testing.T) {
	for _, name := range []string{"../config.yaml", "../config.example.yaml"} {
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		text := string(raw)
		start := strings.Index(text, "\nclaude:\n")
		end := strings.Index(text[start+1:], "\nemail:\n")
		if start < 0 || end < 0 {
			t.Fatalf("%s does not contain the expected Claude section", name)
		}
		claudeSection := text[start : start+1+end]
		for _, legacy := range []string{"\n  provider:", "\n  base_url:", "\n  auth_token:", "\n  models:", "\n  model_usage_aliases:", "\n    ANTHROPIC_"} {
			if strings.Contains(claudeSection, legacy) {
				t.Fatalf("%s contains legacy Claude provider config %q", name, legacy)
			}
		}
	}
}
