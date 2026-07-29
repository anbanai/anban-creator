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
  execution_profiles:
    balanced:
      display_name: 平衡型
      model_name: 豆包
      provider: volcengine_ark
      model_id: doubao-seed-evolving
      protocol: anthropic
      base_url: https://ark.example.com
      auth_token: profile-secret-token
      model_usage_aliases:
        doubao-seed-evolving-latest-version: doubao-seed-evolving
      min_tier: pro
  executor: docker
  execution_token_secret: 0123456789abcdef0123456789abcdef
  runtime_images:
    article: creator-agent-article:latest
    seednote: creator-agent-seednote:latest
    montage: creator-agent-montage:latest
  env:
    CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC: "1"
    CLAUDE_CODE_DISABLE_AUTO_MEMORY: "0"
    CLAUDE_CODE_AUTO_COMPACT_WINDOW: "1000000"
`

func validClaudeConfigForTest() ClaudeConfig {
	return ClaudeConfig{
		ExecutionProfiles: map[string]ClaudeExecutionProfileConfig{
			"balanced": {
				DisplayName: "平衡型", ModelName: "豆包", Provider: "volcengine_ark",
				ModelID: "doubao-seed-evolving", Protocol: "anthropic", BaseURL: "https://ark.example.com",
				AuthToken: "profile-secret-token", MinTier: model.TierPro,
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
	if cfg.Claude.Executor != "docker" || cfg.Claude.Env["CLAUDE_CODE_DISABLE_AUTO_MEMORY"] != "0" {
		t.Fatalf("Claude runtime config = %#v", cfg.Claude)
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

func TestClaudeConfigRejectsProviderEnvAndUnknownRuntimeControls(t *testing.T) {
	for _, test := range []struct{ key, want string }{
		{key: "ANTHROPIC_MODEL", want: "must be configured by claude.execution_profiles"},
		{key: "CLAUDE_CODE_UNKNOWN", want: "is not an allowed Claude runtime control"},
	} {
		t.Run(test.key, func(t *testing.T) {
			body := strings.Replace(validClaudeConfigYAML, "  env:\n", "  env:\n    "+test.key+": value\n", 1)
			_, err := loadClaudeConfigYAML(t, body)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("NewConfig() error = %v, want %q", err, test.want)
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
