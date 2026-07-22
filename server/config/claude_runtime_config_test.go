package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
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
  provider: volcengine_ark
  base_url: https://ark.cn-beijing.volces.com/api/compatible
  auth_token: ${CLAUDE_CODE_AUTH_TOKEN}
  models:
    default: doubao-seed-evolving
    opus: doubao-seed-evolving
    fable: doubao-seed-evolving
    sonnet: doubao-seed-2-1-pro-260628
    haiku: doubao-seed-2-1-turbo-260628
  model_usage_aliases:
    doubao-seed-evolving-latest-version: doubao-seed-evolving
  executor: docker
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
		Provider:  ClaudeProviderVolcengineArk,
		BaseURL:   ClaudeArkCompatibleBaseURL,
		AuthToken: "test-auth-token",
		Models: ClaudeModelsConfig{
			Default: "doubao-seed-evolving",
			Opus:    "doubao-seed-evolving",
			Fable:   "doubao-seed-evolving",
			Sonnet:  "doubao-seed-2-1-pro-260628",
			Haiku:   "doubao-seed-2-1-turbo-260628",
		},
		UsageAliases: map[string]string{"doubao-seed-evolving-latest-version": "doubao-seed-evolving"},
		Executor:     "docker",
		RuntimeImages: RuntimeImages{
			model.PlatformArticle:  "creator-agent-article:latest",
			model.PlatformSeednote: "creator-agent-seednote:latest",
			model.PlatformMontage:  "creator-agent-montage:latest",
		},
	}
}

func loadClaudeConfigYAML(t *testing.T, body string) (*Config, error) {
	t.Helper()
	t.Setenv("CLAUDE_CODE_AUTH_TOKEN", "ark-secret-token")
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return NewConfig(path)
}

func TestClaudeConfigBuildsTypedDirectArkRuntime(t *testing.T) {
	cfg, err := loadClaudeConfigYAML(t, validClaudeConfigYAML)
	if err != nil {
		t.Fatalf("NewConfig() error = %v", err)
	}
	if cfg.Claude.Provider != "volcengine_ark" || cfg.Claude.BaseURL != "https://ark.cn-beijing.volces.com/api/compatible" || cfg.Claude.AuthToken != "ark-secret-token" {
		t.Fatalf("typed Claude provider = %#v", cfg.Claude)
	}
	wantModels := ClaudeModelsConfig{
		Default: "doubao-seed-evolving",
		Opus:    "doubao-seed-evolving",
		Fable:   "doubao-seed-evolving",
		Sonnet:  "doubao-seed-2-1-pro-260628",
		Haiku:   "doubao-seed-2-1-turbo-260628",
	}
	if cfg.Claude.Models != wantModels {
		t.Fatalf("models = %#v, want %#v", cfg.Claude.Models, wantModels)
	}
	wantRuntime := map[string]string{
		"ANTHROPIC_BASE_URL":                       "https://ark.cn-beijing.volces.com/api/compatible",
		"ANTHROPIC_AUTH_TOKEN":                     "ark-secret-token",
		"ANTHROPIC_MODEL":                          "doubao-seed-evolving",
		"ANTHROPIC_DEFAULT_OPUS_MODEL":             "doubao-seed-evolving",
		"ANTHROPIC_DEFAULT_FABLE_MODEL":            "doubao-seed-evolving",
		"ANTHROPIC_DEFAULT_SONNET_MODEL":           "doubao-seed-2-1-pro-260628",
		"ANTHROPIC_DEFAULT_HAIKU_MODEL":            "doubao-seed-2-1-turbo-260628",
		"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1",
		"CLAUDE_CODE_DISABLE_AUTO_MEMORY":          "0",
		"CLAUDE_CODE_AUTO_COMPACT_WINDOW":          "1000000",
	}
	if got := cfg.Claude.RuntimeEnv(); !reflect.DeepEqual(got, wantRuntime) {
		t.Fatalf("RuntimeEnv() = %#v, want %#v", got, wantRuntime)
	}
	wantAliases := map[string]model.ModelUsageIdentity{
		"doubao-seed-evolving":                {Provider: "volcengine_ark", Model: "doubao-seed-evolving"},
		"doubao-seed-evolving-latest-version": {Provider: "volcengine_ark", Model: "doubao-seed-evolving"},
		"doubao-seed-2-1-pro-260628":          {Provider: "volcengine_ark", Model: "doubao-seed-2-1-pro-260628"},
		"doubao-seed-2-1-turbo-260628":        {Provider: "volcengine_ark", Model: "doubao-seed-2-1-turbo-260628"},
	}
	if got := cfg.Claude.RuntimeModelUsageAliases(); !reflect.DeepEqual(got, wantAliases) {
		t.Fatalf("ModelUsageAliases() = %#v, want %#v", got, wantAliases)
	}
}

func TestClaudeConfigRejectsLegacyDuplicateUnknownAndInvalidValues(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "legacy model", body: strings.Replace(validClaudeConfigYAML, "  provider: volcengine_ark", "  model: legacy\n  provider: volcengine_ark", 1), want: "claude.model"},
		{name: "provider key in env", body: strings.Replace(validClaudeConfigYAML, "  env:\n", "  env:\n    ANTHROPIC_MODEL: duplicate\n", 1), want: "claude.env.ANTHROPIC_MODEL"},
		{name: "unknown field", body: strings.Replace(validClaudeConfigYAML, "  provider: volcengine_ark", "  typo_provider: ark\n  provider: volcengine_ark", 1), want: "unknown claude config field"},
		{name: "empty provider", body: strings.Replace(validClaudeConfigYAML, "provider: volcengine_ark", "provider: ''", 1), want: "claude.provider is required"},
		{name: "wrong provider", body: strings.Replace(validClaudeConfigYAML, "provider: volcengine_ark", "provider: ark_gateway", 1), want: "claude.provider must be volcengine_ark"},
		{name: "empty base URL", body: strings.Replace(validClaudeConfigYAML, "base_url: https://ark.cn-beijing.volces.com/api/compatible", "base_url: ''", 1), want: "claude.base_url is required"},
		{name: "gateway base URL", body: strings.Replace(validClaudeConfigYAML, "base_url: https://ark.cn-beijing.volces.com/api/compatible", "base_url: https://gateway.example.com/anthropic", 1), want: "claude.base_url must be https://ark.cn-beijing.volces.com/api/compatible"},
		{name: "empty auth token", body: strings.Replace(validClaudeConfigYAML, "auth_token: ${CLAUDE_CODE_AUTH_TOKEN}", "auth_token: ''", 1), want: "claude.auth_token is required"},
		{name: "empty role model", body: strings.Replace(validClaudeConfigYAML, "fable: doubao-seed-evolving", "fable: ''", 1), want: "claude.models.fable is required"},
		{name: "invalid model suffix", body: strings.Replace(validClaudeConfigYAML, "fable: doubao-seed-evolving", "fable: doubao-seed-evolving[1M]", 1), want: "must not contain [1M]"},
		{name: "alias target is not configured", body: strings.Replace(validClaudeConfigYAML, "doubao-seed-evolving-latest-version: doubao-seed-evolving", "doubao-seed-evolving-latest-version: unknown-model", 1), want: "must target a configured Claude model"},
		{name: "missing verified latest alias", body: strings.Replace(validClaudeConfigYAML, "  model_usage_aliases:\n    doubao-seed-evolving-latest-version: doubao-seed-evolving\n", "", 1), want: "claude.model_usage_aliases.doubao-seed-evolving-latest-version is required"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := loadClaudeConfigYAML(t, test.body)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("NewConfig() error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestClaudeConfigRejectsWrongRoleModels(t *testing.T) {
	tests := []struct {
		role string
		want string
	}{
		{role: "default", want: "doubao-seed-evolving"},
		{role: "opus", want: "doubao-seed-evolving"},
		{role: "fable", want: "doubao-seed-evolving"},
		{role: "sonnet", want: "doubao-seed-2-1-pro-260628"},
		{role: "haiku", want: "doubao-seed-2-1-turbo-260628"},
	}
	for _, test := range tests {
		t.Run(test.role, func(t *testing.T) {
			body := strings.Replace(validClaudeConfigYAML, test.role+": "+test.want, test.role+": wrong-model", 1)
			_, err := loadClaudeConfigYAML(t, body)
			wantError := "claude.models." + test.role + " must be " + test.want
			if err == nil || !strings.Contains(err.Error(), wantError) {
				t.Fatalf("NewConfig() error = %v, want containing %q", err, wantError)
			}
		})
	}
}

func TestClaudeConfigUsesSharedModelUsageAliasValidation(t *testing.T) {
	manyAliases := strings.Builder{}
	manyAliases.WriteString("  model_usage_aliases:\n    doubao-seed-evolving-latest-version: doubao-seed-evolving\n")
	for i := 0; i < 129; i++ {
		manyAliases.WriteString("    raw-")
		manyAliases.WriteString(strconv.Itoa(i))
		manyAliases.WriteString(": doubao-seed-evolving\n")
	}
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "raw alias contains equals",
			body: strings.Replace(validClaudeConfigYAML,
				"    doubao-seed-evolving-latest-version: doubao-seed-evolving\n",
				"    doubao-seed-evolving-latest-version: doubao-seed-evolving\n    \"bad=raw\": doubao-seed-evolving\n", 1),
			want: `model usage alias "bad=raw" is invalid`,
		},
		{
			name: "alias count exceeds limit",
			body: strings.Replace(validClaudeConfigYAML,
				"  model_usage_aliases:\n    doubao-seed-evolving-latest-version: doubao-seed-evolving\n",
				manyAliases.String(), 1),
			want: "model usage alias count exceeds limit",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := loadClaudeConfigYAML(t, test.body)
			if err == nil || !strings.Contains(err.Error(), "claude.model_usage_aliases") || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("NewConfig() error = %v, want claude.model_usage_aliases context containing %q", err, test.want)
			}
		})
	}
}

func TestClaudeConfigRedactsAuthTokenFromStringAndJSON(t *testing.T) {
	cfg, err := loadClaudeConfigYAML(t, validClaudeConfigYAML)
	if err != nil {
		t.Fatal(err)
	}
	formatted := fmt.Sprintf("%v", cfg.Claude)
	detailed := fmt.Sprintf("%+v", cfg.Claude)
	goSyntax := fmt.Sprintf("%#v", cfg.Claude)
	raw, err := json.Marshal(cfg.Claude)
	if err != nil {
		t.Fatal(err)
	}
	for _, output := range []string{formatted, detailed, goSyntax, string(raw)} {
		if strings.Contains(output, "ark-secret-token") {
			t.Fatalf("Claude config leaked auth token: %s", output)
		}
	}
}

func TestProductionClaudeConfigHasNoLegacyProviderEnvOrInvalidSuffix(t *testing.T) {
	for _, name := range []string{"../config.yaml", "../config.example.yaml"} {
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		text := string(raw)
		if strings.Contains(text, "[1M]") {
			t.Fatalf("%s contains invalid [1M] suffix", name)
		}
		start := strings.Index(text, "\nclaude:\n")
		end := strings.Index(text[start+1:], "\nemail:\n")
		if start < 0 || end < 0 {
			t.Fatalf("%s does not contain the expected Claude section", name)
		}
		claudeSection := text[start : start+1+end]
		for _, legacy := range []string{"\n  model:", "\n    ANTHROPIC_AUTH_TOKEN:", "\n    ANTHROPIC_BASE_URL:", "\n    ANTHROPIC_MODEL:"} {
			if strings.Contains(claudeSection, legacy) {
				t.Fatalf("%s contains legacy Claude provider config %q", name, legacy)
			}
		}
	}
}
