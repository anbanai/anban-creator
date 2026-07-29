package config

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestAgentProfilesUseProviderRegistryAndModelMatrices(t *testing.T) {
	for _, name := range []string{"config.yaml", "config.example.yaml"} {
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("..", name))
			if err != nil {
				t.Fatal(err)
			}
			var configured struct {
				Claude ClaudeConfig `yaml:"claude"`
			}
			if err := yaml.Unmarshal(raw, &configured); err != nil {
				t.Fatal(err)
			}
			provider := configured.Claude.Providers["volcengine_ark"]
			if provider.AuthToken != "${ANBAN_DOUBAO_AGENT_API_KEY}" || provider.BaseURL != "${ANBAN_DOUBAO_AGENT_BASE_URL:-https://ark.cn-beijing.volces.com/api/compatible}" {
				t.Fatalf("balanced provider must use dedicated Agent credentials, got base_url=%q auth_token=%q", provider.BaseURL, provider.AuthToken)
			}
			balanced := configured.Claude.ExecutionProfiles["balanced"]
			if balanced.Provider != "volcengine_ark" || balanced.Models.Default != "doubao-seed-evolving" || balanced.Models.Opus == "" || balanced.Models.Fable == "" || balanced.Models.Sonnet == "" || balanced.Models.Haiku == "" {
				t.Fatalf("balanced profile matrix = %#v", balanced)
			}
			if balanced.ModelUsageAliases["doubao-seed-evolving-latest-version"] != "doubao-seed-evolving" {
				t.Fatalf("balanced profile model_usage_aliases = %#v", balanced.ModelUsageAliases)
			}
		})
	}
}
