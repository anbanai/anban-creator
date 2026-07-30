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
			doubao := configured.Claude.Providers["volcengine_ark"]
			if doubao.AuthToken != "${ANBAN_DOUBAO_AGENT_API_KEY}" || doubao.BaseURL != "${ANBAN_DOUBAO_AGENT_BASE_URL:-https://ark.cn-beijing.volces.com/api/compatible}" {
				t.Fatalf("Doubao provider credentials = %#v", doubao)
			}
			costEffective := configured.Claude.ExecutionProfiles["cost_effective"]
			if costEffective.Provider != "deepseek" || costEffective.Models.Default != "deepseek-v4-flash" || costEffective.Models.Fable != "deepseek-v4-flash" || costEffective.Models.Opus != "deepseek-v4-pro" || costEffective.Models.Sonnet != "deepseek-v4-pro" || costEffective.Models.Haiku != "deepseek-v4-flash" {
				t.Fatalf("cost_effective profile matrix = %#v", costEffective)
			}
			if costEffective.Claude.EffortLevel == nil || *costEffective.Claude.EffortLevel != "medium" || costEffective.Claude.MaxContextTokens == nil || *costEffective.Claude.MaxContextTokens != 1048576 || costEffective.Claude.MaxOutputTokens == nil || *costEffective.Claude.MaxOutputTokens != 393216 || costEffective.Claude.AutoCompactWindow == nil || *costEffective.Claude.AutoCompactWindow != 1048576 {
				t.Fatalf("cost_effective Claude controls = %#v", costEffective.Claude)
			}
			balanced := configured.Claude.ExecutionProfiles["balanced"]
			if balanced.Provider != "zhipu" || balanced.Models.Default != "glm-5.2" || balanced.Models.Opus != "glm-5.2" || balanced.Models.Fable != "glm-5.2" || balanced.Models.Sonnet != "glm-5.2" || balanced.Models.Haiku != "glm-5.2" {
				t.Fatalf("balanced profile matrix = %#v", balanced)
			}
			if balanced.ModelUsageAliases["glm-5.2"] != "glm-5.2" || balanced.Claude.MaxOutputTokens == nil || *balanced.Claude.MaxOutputTokens != 131072 {
				t.Fatalf("balanced profile model_usage_aliases = %#v", balanced.ModelUsageAliases)
			}
			maximumQuality := configured.Claude.ExecutionProfiles["maximum_quality"]
			if maximumQuality.Provider != "moonshot" || maximumQuality.Models.Default != "kimi-k3[1m]" || maximumQuality.Claude.AlwaysEnableEffort == nil || !*maximumQuality.Claude.AlwaysEnableEffort || maximumQuality.Claude.SubagentModel == nil || *maximumQuality.Claude.SubagentModel != "kimi-k3[1m]" {
				t.Fatalf("maximum_quality profile = %#v", maximumQuality)
			}
			if _, exists := maximumQuality.ModelUsageAliases["kimi-k2.7-code-highspeed"]; exists {
				t.Fatalf("maximum_quality must not advertise an unused K2.7 highspeed alias: %#v", maximumQuality.ModelUsageAliases)
			}
		})
	}
}
