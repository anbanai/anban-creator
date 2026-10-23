package service

import (
	"testing"

	"github.com/anbanai/anban-creator/server/billing"
	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
)

func TestTokenPlayerProfilesUseMoonshotCosts(t *testing.T) {
	bundle, err := billing.LoadBundle("../billing")
	if err != nil {
		t.Fatal(err)
	}
	calculator := NewProviderCostCalculator(bundle.Costs)
	for _, provider := range []string{"tokenplayer_anthropic", "tokenplayer_openai"} {
		for _, tc := range []struct {
			model string
			cost  int64
		}{
			{model: "kimi-k3", cost: 142_000_000},
			{model: "kimi-k2.7-code", cost: 41_300_000},
			{model: "kimi-k2.7-code-highspeed", cost: 82_600_000},
		} {
			t.Run(provider+"/"+tc.model, func(t *testing.T) {
				profile := configuredAgentProfile(provider, "", "https://tokenplayer.cn", "test-token", tc.model+"[1m]", tc.model)
				registry, err := NewAgentProfileRegistryFromConfig(map[string]srvconfig.ClaudeExecutionProfileConfig{"quality": profile}, bundle.Costs)
				if err != nil {
					t.Fatal(err)
				}
				resolved, err := registry.ResolveForTier("quality", model.TierEnterprise)
				if err != nil {
					t.Fatalf("enterprise quality profile unavailable: %v", err)
				}
				modelID := resolved.Provider + "/" + resolved.ModelUsageAliases[tc.model+"[1m]"]
				cost, err := calculator.TokenCost(modelID, TokenUsage{
					Input: 1_000_000, CacheRead: 1_000_000, CacheCreation: 1_000_000, Output: 1_000_000,
				})
				if err != nil {
					t.Fatal(err)
				}
				if cost.MicroCNY != tc.cost || cost.ModelID != provider+"/"+tc.model {
					t.Fatalf("cost = %d for %q, want %d for %q", cost.MicroCNY, cost.ModelID, tc.cost, provider+"/"+tc.model)
				}
			})
		}
	}
}

func TestTokenPlayerDeepseekFlashProfileUsesConfiguredCostAlias(t *testing.T) {
	bundle, err := billing.LoadBundle("../billing")
	if err != nil {
		t.Fatal(err)
	}

	profile := configuredAgentProfile("tokenplayer_anthropic", "low cost", "https://tokenplayer.cn", "test-token", "deepseek-flash[1m]", "deepseek-v4.1-flash")
	profile.Envs["ANTHROPIC_DEFAULT_OPUS_MODEL"] = "deepseek-flash[1m]"
	profile.Envs["ANTHROPIC_DEFAULT_FABLE_MODEL"] = "deepseek-flash"
	profile.Envs["ANTHROPIC_DEFAULT_SONNET_MODEL"] = "deepseek-flash[1m]"
	profile.Envs["ANTHROPIC_DEFAULT_HAIKU_MODEL"] = "deepseek-flash"
	profile.Envs["CLAUDE_CODE_SUBAGENT_MODEL"] = "deepseek-flash"
	profile.ModelUsageAliases["deepseek-flash"] = "deepseek-v4.1-flash"

	registry, err := NewAgentProfileRegistryFromConfig(map[string]srvconfig.ClaudeExecutionProfileConfig{"effective": profile}, bundle.Costs)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := registry.ResolveForTier("effective", model.TierFree)
	if err != nil {
		t.Fatalf("effective profile unavailable: %v", err)
	}

	cost, err := NewProviderCostCalculator(bundle.Costs).TokenCost("tokenplayer_anthropic/"+resolved.ModelUsageAliases["deepseek-flash[1m]"], TokenUsage{
		Input: 1_000_000, CacheRead: 1_000_000, CacheCreation: 1_000_000, Output: 1_000_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cost.ModelID != "tokenplayer_anthropic/deepseek-v4.1-flash" || cost.MicroCNY != 4_052_160 {
		t.Fatalf("cost = %d for %q, want 4052160 for tokenplayer_anthropic/deepseek-v4.1-flash", cost.MicroCNY, cost.ModelID)
	}
}
