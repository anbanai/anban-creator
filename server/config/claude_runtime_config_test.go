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
    quality:
      provider: moonshot
      description: flagship
      envs:
        ANTHROPIC_BASE_URL: "https://api.moonshot.cn/anthropic"
        ANTHROPIC_AUTH_TOKEN: "profile-secret-token"
        ANTHROPIC_MODEL: "kimi-k3[1m]"
        ANTHROPIC_DEFAULT_OPUS_MODEL: "kimi-k3[1m]"
        ANTHROPIC_DEFAULT_FABLE_MODEL: "kimi-k3[1m]"
        ANTHROPIC_DEFAULT_SONNET_MODEL: "kimi-k3[1m]"
        ANTHROPIC_DEFAULT_HAIKU_MODEL: "kimi-k3[1m]"
        MAX_THINKING_TOKENS: "0"
        ENABLE_TOOL_SEARCH: "false"
      model_usage_aliases:
        kimi-k3: kimi-k3
        kimi-k3[1m]: kimi-k3
  executor: docker
  execution_token_secret: 0123456789abcdef0123456789abcdef
  runtime_images:
    article: creator-agent-article:latest
    seednote: creator-agent-seednote:latest
    montage: creator-agent-montage:latest
`

func validClaudeConfigForTest() ClaudeConfig {
	return ClaudeConfig{
		ExecutionProfiles: map[string]ClaudeExecutionProfileConfig{
			"quality": {
				Provider: "moonshot", Description: "flagship",
				Envs: map[string]string{
					model.ClaudeEnvBaseURL:           "https://api.moonshot.cn/anthropic",
					model.ClaudeEnvAuthToken:         "profile-secret-token",
					model.ClaudeEnvModel:             "kimi-k3[1m]",
					"ANTHROPIC_DEFAULT_OPUS_MODEL":   "kimi-k3[1m]",
					"ANTHROPIC_DEFAULT_FABLE_MODEL":  "kimi-k3[1m]",
					"ANTHROPIC_DEFAULT_SONNET_MODEL": "kimi-k3[1m]",
					"ANTHROPIC_DEFAULT_HAIKU_MODEL":  "kimi-k3[1m]",
				},
				ModelUsageAliases: map[string]string{"kimi-k3": "kimi-k3", "kimi-k3[1m]": "kimi-k3"},
			},
		},
		Executor: "docker", ExecutionTokenSecret: "0123456789abcdef0123456789abcdef",
		RuntimeImages: RuntimeImages{
			model.PlatformArticle: "creator-agent-article:latest", model.PlatformSeednote: "creator-agent-seednote:latest", model.PlatformMontage: "creator-agent-montage:latest",
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

func TestClaudeConfigAcceptsOnlyProfileEnvs(t *testing.T) {
	cfg, err := loadClaudeConfigYAML(t, validClaudeConfigYAML)
	if err != nil {
		t.Fatal(err)
	}
	profile := cfg.Claude.ExecutionProfiles["quality"]
	if profile.Envs[model.ClaudeEnvModel] != "kimi-k3[1m]" || profile.Envs["MAX_THINKING_TOKENS"] != "0" || profile.Envs["ENABLE_TOOL_SEARCH"] != "false" {
		t.Fatalf("profile envs = %#v", profile.Envs)
	}
}

func TestClaudeConfigAllowsOptionalProfileEnvsToBeOmitted(t *testing.T) {
	cfg, err := loadClaudeConfigYAML(t, strings.ReplaceAll(validClaudeConfigYAML, "        MAX_THINKING_TOKENS: \"0\"\n        ENABLE_TOOL_SEARCH: \"false\"\n", ""))
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := cfg.Claude.ExecutionProfiles["quality"].Envs["MAX_THINKING_TOKENS"]; exists {
		t.Fatal("omitted optional env was synthesized")
	}
}

func TestClaudeConfigRejectsObsoleteProfileSchema(t *testing.T) {
	tests := []struct{ name, old, replacement, wantPath string }{
		{name: "providers", old: "claude:\n", replacement: "claude:\n  providers: {}\n", wantPath: `unknown claude config field "providers"`},
		{name: "models", old: "      envs:\n", replacement: "      models: {}\n      envs:\n", wantPath: "claude.execution_profiles.quality.models"},
		{name: "claude controls", old: "      envs:\n", replacement: "      claude: {}\n      envs:\n", wantPath: "claude.execution_profiles.quality.claude"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadClaudeConfigYAML(t, strings.Replace(validClaudeConfigYAML, tt.old, tt.replacement, 1))
			if err == nil || !strings.Contains(err.Error(), tt.wantPath) {
				t.Fatalf("NewConfig() error = %v, want path %q", err, tt.wantPath)
			}
		})
	}
}

func TestClaudeProfileConfigRejectsUnknownEnvWithFullPath(t *testing.T) {
	body := strings.Replace(validClaudeConfigYAML, "        ANTHROPIC_MODEL:", "        PATH: \"/tmp\"\n        ANTHROPIC_MODEL:", 1)
	_, err := loadClaudeConfigYAML(t, body)
	if err == nil || !strings.Contains(err.Error(), "claude.execution_profiles.quality.envs.PATH") {
		t.Fatalf("NewConfig() error = %v", err)
	}
}

func TestClaudeProfileConfigRequiresStringEnvValues(t *testing.T) {
	body := strings.Replace(validClaudeConfigYAML, "        MAX_THINKING_TOKENS: \"0\"", "        MAX_THINKING_TOKENS: 0", 1)
	_, err := loadClaudeConfigYAML(t, body)
	if err == nil || !strings.Contains(err.Error(), "claude.execution_profiles.quality.envs.MAX_THINKING_TOKENS") {
		t.Fatalf("NewConfig() error = %v", err)
	}
}

func TestClaudeProfileConfigReportsInvalidEnvPath(t *testing.T) {
	body := strings.Replace(validClaudeConfigYAML, "https://api.moonshot.cn/anthropic", "http://api.moonshot.cn/anthropic", 1)
	_, err := loadClaudeConfigYAML(t, body)
	if err == nil || !strings.Contains(err.Error(), "claude.execution_profiles.quality.envs") {
		t.Fatalf("NewConfig() error = %v", err)
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

func TestProductionClaudeConfigUsesExecutionProfileEnvsOnly(t *testing.T) {
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
		section := text[start : start+1+end]
		for _, obsolete := range []string{"\n  providers:", "\n      models:", "\n      claude:", "cost_effective:", "maximum_quality:"} {
			if strings.Contains(section, obsolete) {
				t.Fatalf("%s contains obsolete Claude config %q", name, obsolete)
			}
		}
	}
}
