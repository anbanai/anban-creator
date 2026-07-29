package config

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestBalancedAgentProfileUsesDedicatedDeploymentCredentials(t *testing.T) {
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
			balanced := configured.Claude.ExecutionProfiles["balanced"]
			if balanced.ModelName != "豆包 Seed Evolving" {
				t.Fatalf("balanced profile model_name = %q, want exact product model name", balanced.ModelName)
			}
			if balanced.AuthToken != "${ANBAN_DOUBAO_AGENT_API_KEY}" || balanced.BaseURL != "${ANBAN_DOUBAO_AGENT_BASE_URL:-https://ark.cn-beijing.volces.com/api/compatible}" {
				t.Fatalf("balanced profile must use dedicated Agent credentials, got base_url=%q auth_token=%q", balanced.BaseURL, balanced.AuthToken)
			}
			if balanced.ModelUsageAliases["doubao-seed-evolving-latest-version"] != "doubao-seed-evolving" {
				t.Fatalf("balanced profile model_usage_aliases = %#v", balanced.ModelUsageAliases)
			}
		})
	}
}
